package notify

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	recordSize = 4096

	maxPushPayload = 2048
	webPushTimeout = 10 * time.Second
)

type WebPushSender struct {
	vapid   *VAPID
	guard   EndpointGuard
	http    *http.Client
	baseURL string
	now     func() time.Time
	random  io.Reader
}

func NewWebPushSender(vapid *VAPID, guard EndpointGuard, baseURL string) *WebPushSender {
	return &WebPushSender{
		vapid:   vapid,
		guard:   guard,
		http:    guard.HTTPClient(webPushTimeout),
		baseURL: strings.TrimRight(baseURL, "/"),
		now:     time.Now,
		random:  rand.Reader,
	}
}

type declarativePush struct {
	WebPush      int                     `json:"web_push"`
	Notification declarativeNotification `json:"notification"`
}

type declarativeNotification struct {
	Title    string            `json:"title"`
	Body     string            `json:"body,omitempty"`
	Navigate string            `json:"navigate"`
	Tag      string            `json:"tag,omitempty"`
	Lang     string            `json:"lang"`
	Silent   bool              `json:"silent"`
	Data     map[string]string `json:"data"`
}

func (s *WebPushSender) payload(message Message) ([]byte, error) {
	navigate := s.baseURL + "/"
	if message.URL != "" && message.URL != "/" {
		navigate += "?open=" + url.QueryEscape(message.URL)
	}
	push := declarativePush{
		WebPush: 8030,
		Notification: declarativeNotification{
			Title:    message.Title,
			Body:     message.Body,
			Navigate: navigate,
			Tag:      message.CollapseKey,
			Lang:     "ru",
			Data:     map[string]string{"url": message.URL, "kind": message.Kind},
		},
	}
	for {
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(push); err != nil {
			return nil, fmt.Errorf("encode web push payload: %w", err)
		}
		out := bytes.TrimRight(buf.Bytes(), "\n")
		excess := len(out) - maxPushPayload
		if excess <= 0 {
			return out, nil
		}
		body := push.Notification.Body
		if body == "" {
			return nil, errors.New("web push payload too large even without a body")
		}

		keep := utf8.RuneCountInString(body) - excess/2 - 2
		if keep < 2 {
			push.Notification.Body = ""
		} else {
			push.Notification.Body = cut(body, keep)
		}
	}
}

func urgency(kind string) string {
	switch kind {
	case KindReminder, KindTest:
		return "high"
	default:
		return "normal"
	}
}

func topic(collapseKey string) string {
	if collapseKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(collapseKey))
	return hex.EncodeToString(sum[:16])
}

func (s *WebPushSender) Send(ctx context.Context, target Target, message Message) error {
	endpoint, err := s.guard.Check(target.Endpoint)
	if err != nil {
		return fmt.Errorf("%w: endpoint not accepted", ErrTokenGone)
	}
	uaPublic, errKey := decodeBase64(target.P256dh)
	authSecret, errAuth := decodeBase64(target.Auth)
	if errKey != nil || errAuth != nil {
		return fmt.Errorf("%w: subscription keys unreadable", ErrTokenGone)
	}

	plaintext, err := s.payload(message)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(s.random, salt); err != nil {
		return retryable(fmt.Errorf("web push: salt: %w", err), 0)
	}
	local, err := ecdh.P256().GenerateKey(s.random)
	if err != nil {
		return retryable(fmt.Errorf("web push: ephemeral key: %w", err), 0)
	}
	body, err := encrypt(plaintext, uaPublic, authSecret, salt, local)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTokenGone, err)
	}

	now := s.now()
	authorization, err := s.vapid.authorization(endpoint, now)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("web push: build request: %w", err)
	}
	ttl := max(message.ExpiresAt.Sub(now), 0)
	req.Header.Set("TTL", strconv.FormatInt(int64(ttl/time.Second), 10))
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Urgency", urgency(message.Kind))
	if t := topic(message.CollapseKey); t != "" {
		req.Header.Set("Topic", t)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		cause := err
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			cause = urlErr.Err
		}
		if errors.Is(cause, errPrivateAddress) {
			return fmt.Errorf("%w: %w", ErrTokenGone, errPrivateAddress)
		}
		return retryable(fmt.Errorf("web push: send to %s: %w", endpoint.Hostname(), cause), 0)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	switch code := resp.StatusCode; {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusNotFound || code == http.StatusGone:
		return fmt.Errorf("%w: %s answered %d", ErrTokenGone, endpoint.Hostname(), code)
	case code == http.StatusTooManyRequests || code >= 500:
		return retryable(fmt.Errorf("web push: %s answered %d", endpoint.Hostname(), code),
			retryAfter(resp.Header.Get("Retry-After"), now))
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("web push: %s answered %d %s (check the VAPID keys and subject)",
			endpoint.Hostname(), code, reason(raw))
	default:
		return fmt.Errorf("web push: %s answered %d %s", endpoint.Hostname(), code, reason(raw))
	}
}

func reason(raw []byte) string {
	var parsed struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &parsed) == nil {
		if parsed.Reason != "" {
			return parsed.Reason
		}
		if parsed.Message != "" {
			return cut(parsed.Message, 200)
		}
	}
	return cut(strings.TrimSpace(string(raw)), 200)
}

func encrypt(plaintext, uaPublic, authSecret, salt []byte, local *ecdh.PrivateKey) ([]byte, error) {
	if len(authSecret) != 16 {
		return nil, errors.New("auth secret must be 16 bytes")
	}
	if len(salt) != 16 {
		return nil, errors.New("salt must be 16 bytes")
	}
	if len(plaintext)+1+16 > recordSize {
		return nil, errors.New("web push payload does not fit one record")
	}
	remote, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("subscription key: %w", err)
	}
	shared, err := local.ECDH(remote)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}
	asPublic := local.PublicKey().Bytes()

	prkKey, err := hkdf.Extract(sha256.New, shared, authSecret)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	header := make([]byte, 0, 16+4+1+len(asPublic)+len(plaintext)+1+gcm.Overhead())
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)

	record := make([]byte, 0, len(plaintext)+1)
	record = append(record, plaintext...)
	record = append(record, 0x02)
	return gcm.Seal(header, nonce, record, nil), nil
}
