package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

	FCMEndpoint = "https://fcm.googleapis.com"

	fcmTimeout = 10 * time.Second
)

type FCMSender struct {
	projectID string
	tokens    oauth2.TokenSource
	endpoint  string
	http      *http.Client
	now       func() time.Time
}

func NewFCMSender(projectID string, tokens oauth2.TokenSource, endpoint string) *FCMSender {
	if endpoint == "" {
		endpoint = FCMEndpoint
	}
	return &FCMSender{
		projectID: projectID,
		tokens:    tokens,
		endpoint:  strings.TrimRight(endpoint, "/"),
		http:      &http.Client{Timeout: fcmTimeout},
		now:       time.Now,
	}
}

var ErrNoCredentials = errors.New("FCM credentials file not found")

func LoadFCMSender(path, endpoint string) (*FCMSender, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("read FCM credentials: %w", err)
	}
	var file struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse FCM credentials: %w", err)
	}
	if file.ProjectID == "" {
		return nil, errors.New("FCM credentials have no project_id")
	}
	config, err := google.JWTConfigFromJSON(data, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("FCM credentials: %w", err)
	}

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: fcmTimeout})
	return NewFCMSender(file.ProjectID, config.TokenSource(ctx), endpoint), nil
}

func (s *FCMSender) ProjectID() string { return s.projectID }

func (s *FCMSender) Ping(context.Context) error {
	_, err := s.tokens.Token()
	return err
}

type fcmRequest struct {
	Message fcmMessage `json:"message"`
}

type fcmMessage struct {
	Token        string            `json:"token"`
	Notification fcmNotification   `json:"notification"`
	Data         map[string]string `json:"data"`
	Android      fcmAndroid        `json:"android"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

type fcmAndroid struct {
	Priority     string                 `json:"priority"`
	TTL          string                 `json:"ttl"`
	CollapseKey  string                 `json:"collapse_key,omitempty"`
	Notification fcmAndroidNotification `json:"notification"`
}

type fcmAndroidNotification struct {
	ChannelID   string `json:"channel_id"`
	ClickAction string `json:"click_action"`
}

func androidChannel(kind string) (channel, priority string) {
	switch kind {
	case KindReminder:
		return "reminders", "HIGH"
	case KindDigest:
		return "digest", "NORMAL"
	case KindTest:

		return "general", "HIGH"
	default:
		return "general", "NORMAL"
	}
}

func (s *FCMSender) buildRequest(target Target, message Message) fcmRequest {
	channel, priority := androidChannel(message.Kind)
	ttl := max(message.ExpiresAt.Sub(s.now()).Truncate(time.Second), 0)
	return fcmRequest{Message: fcmMessage{
		Token:        target.Token,
		Notification: fcmNotification{Title: message.Title, Body: message.Body},
		Data:         map[string]string{"url": message.URL, "kind": message.Kind},
		Android: fcmAndroid{
			Priority:    priority,
			TTL:         strconv.FormatInt(int64(ttl/time.Second), 10) + "s",
			CollapseKey: message.CollapseKey,
			Notification: fcmAndroidNotification{
				ChannelID:   channel,
				ClickAction: "FLUTTER_NOTIFICATION_CLICK",
			},
		},
	}}
}

type fcmError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Details []struct {
			Type            string `json:"@type"`
			ErrorCode       string `json:"errorCode"`
			FieldViolations []struct {
				Field string `json:"field"`
			} `json:"fieldViolations"`
		} `json:"details"`
	} `json:"error"`
}

func (e fcmError) errorCode() string {
	for _, d := range e.Error.Details {
		if d.ErrorCode != "" {
			return d.ErrorCode
		}
	}
	return e.Error.Status
}

func (e fcmError) aboutToken() bool {
	for _, d := range e.Error.Details {
		for _, v := range d.FieldViolations {
			if v.Field == "message.token" {
				return true
			}
		}
	}
	return strings.Contains(strings.ToLower(e.Error.Message), "registration token")
}

func (s *FCMSender) Send(ctx context.Context, target Target, message Message) error {
	if target.Token == "" {
		return ErrTokenGone
	}
	access, err := s.tokens.Token()
	if err != nil {
		var refused *oauth2.RetrieveError
		if errors.As(err, &refused) && refused.Response != nil && refused.Response.StatusCode < 500 {
			return fmt.Errorf("fcm: access token refused (%d), check the service account", refused.Response.StatusCode)
		}
		return retryable(fmt.Errorf("fcm: access token: %w", err), 0)
	}

	body, err := json.Marshal(s.buildRequest(target, message))
	if err != nil {
		return fmt.Errorf("fcm: encode: %w", err)
	}
	endpoint := s.endpoint + "/v1/projects/" + url.PathEscape(s.projectID) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	access.SetAuthHeader(req)

	resp, err := s.http.Do(req)
	if err != nil {
		cause := err
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			cause = urlErr.Err
		}
		return retryable(fmt.Errorf("fcm: send: %w", cause), 0)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	var parsed fcmError
	_ = json.Unmarshal(raw, &parsed)
	code := parsed.errorCode()
	failure := fmt.Errorf("fcm answered %d %s: %s", resp.StatusCode, code, strings.TrimSpace(parsed.Error.Message))

	switch {
	case resp.StatusCode == http.StatusNotFound || code == "UNREGISTERED":
		return fmt.Errorf("%w: %w", ErrTokenGone, failure)
	case resp.StatusCode == http.StatusBadRequest && parsed.aboutToken():
		return fmt.Errorf("%w: %w", ErrTokenGone, failure)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return retryable(failure, retryAfter(resp.Header.Get("Retry-After"), s.now()))
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w (check the service account and the project)", failure)
	default:
		return failure
	}
}

func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
