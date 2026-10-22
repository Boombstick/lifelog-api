package notify

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type VAPID struct {
	key       *ecdsa.PrivateKey
	publicKey string
	subject   string
}

const vapidTTL = 12 * time.Hour

func LoadVAPID(publicKey, privateKey, subject string) (*VAPID, error) {
	raw, err := decodeBase64(privateKey)
	if err != nil {
		return nil, fmt.Errorf("VAPID private key: %w", err)
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, fmt.Errorf("VAPID private key: %w", err)
	}
	derived, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, fmt.Errorf("VAPID public key: %w", err)
	}
	given, err := decodeBase64(publicKey)
	if err != nil {
		return nil, fmt.Errorf("VAPID public key: %w", err)
	}
	if string(given) != string(derived) {
		return nil, errors.New("VAPID public key does not belong to the private key")
	}
	if err := checkSubject(subject); err != nil {
		return nil, err
	}
	return &VAPID{
		key:       key,
		publicKey: base64.RawURLEncoding.EncodeToString(derived),
		subject:   subject,
	}, nil
}

func GenerateVAPIDKeys() (publicKey, privateKey string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return "", "", err
	}
	priv, err := key.Bytes()
	if err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(pub), base64.RawURLEncoding.EncodeToString(priv), nil
}

func (v *VAPID) PublicKey() string { return v.publicKey }

func checkSubject(subject string) error {
	bad := fmt.Errorf("VAPID subject %q must be mailto:<address> or https://<host> on a real domain", subject)
	var host string
	switch {
	case strings.HasPrefix(subject, "mailto:"):
		address, err := mail.ParseAddress(strings.TrimPrefix(subject, "mailto:"))
		if err != nil {
			return bad
		}
		host = address.Address[strings.LastIndexByte(address.Address, '@')+1:]
	case strings.HasPrefix(subject, "https://"):
		parsed, err := url.Parse(subject)
		if err != nil {
			return bad
		}
		host = parsed.Hostname()
	default:
		return bad
	}
	host = strings.ToLower(host)
	if !strings.Contains(host, ".") || host == "localhost" ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".invalid") ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".test") {
		return bad
	}
	return nil
}

func (v *VAPID) authorization(endpoint *url.URL, now time.Time) (string, error) {
	claims := jwt.MapClaims{
		"aud": endpoint.Scheme + "://" + endpoint.Host,
		"exp": now.Add(vapidTTL).Unix(),
		"sub": v.subject,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodES256, claims).SignedString(v.key)
	if err != nil {
		return "", fmt.Errorf("sign VAPID token: %w", err)
	}
	return "vapid t=" + signed + ", k=" + v.publicKey, nil
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "=")
	value = strings.NewReplacer("+", "-", "/", "_").Replace(value)
	if value == "" {
		return nil, errors.New("empty")
	}
	return base64.RawURLEncoding.DecodeString(value)
}
