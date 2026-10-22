package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"lifelog-api/internal/httpclient"
	"lifelog-api/internal/notify"
)

type CatalogClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewCatalogClient(baseURL, token string) *CatalogClient {
	return &CatalogClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    httpclient.Internal(20 * time.Second),
	}
}

func (c *CatalogClient) Upcoming(ctx context.Context, from, to time.Time, user uuid.UUID) ([]CatalogEvent, error) {
	query := url.Values{}
	if user != uuid.Nil {
		query.Set("user", user.String())
	}
	query.Set("from", from.UTC().Format("2006-01-02"))

	query.Set("to", to.UTC().AddDate(0, 0, 1).Format("2006-01-02"))

	var payload struct {
		Items []CatalogEvent `json:"items"`
	}
	if err := c.get(ctx, "/internal/calendar/upcoming?"+query.Encode(), &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *CatalogClient) get(ctx context.Context, path string, dst any) error {
	if err := httpclient.Call(ctx, c.http, http.MethodGet, c.baseURL+path, c.token, nil, dst, http.StatusOK); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

type AuthClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewAuthClient(baseURL, token string) *AuthClient {
	return &AuthClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    httpclient.Internal(15 * time.Second),
	}
}

func (c *AuthClient) TelegramIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	if len(userIDs) == 0 {
		return map[uuid.UUID]int64{}, nil
	}

	ids := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		ids = append(ids, id.String())
	}
	var payload struct {
		Links map[string]int64 `json:"links"`
	}
	err := httpclient.Call(ctx, c.http, http.MethodPost, c.baseURL+"/internal/telegram/ids", c.token,
		map[string]any{"user_ids": ids}, &payload, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("auth: telegram ids: %w", err)
	}

	out := make(map[uuid.UUID]int64, len(payload.Links))
	for raw, chatID := range payload.Links {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		out[id] = chatID
	}
	return out, nil
}

type TelegramMessenger struct {
	token   string
	baseURL string
	secret  string
	http    *http.Client
}

func NewTelegramMessenger(token, baseURL, secret string) *TelegramMessenger {
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &TelegramMessenger{
		token:   token,
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (m *TelegramMessenger) Send(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,

		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	return m.call(ctx, http.MethodPost, "sendMessage", body)
}

func (m *TelegramMessenger) call(ctx context.Context, method, apiMethod string, body []byte) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+"/bot"+m.token+"/"+apiMethod, reader)
	if err != nil {
		return fmt.Errorf("telegram %s: cannot build request", apiMethod)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if m.secret != "" {
		req.Header.Set("X-Relay-Secret", m.secret)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		cause := "request failed"
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			cause = urlErr.Err.Error()
		}
		return errors.New(m.redact(fmt.Sprintf("telegram %s %s %s: %s", apiMethod, method, req.URL.Host, cause)))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return errors.New(m.redact(fmt.Sprintf("telegram %s answered %d: %s", apiMethod, resp.StatusCode, strings.TrimSpace(string(raw)))))
	}
	return nil
}

func (m *TelegramMessenger) redact(text string) string {
	if m.token == "" {
		return text
	}
	return strings.ReplaceAll(text, m.token, "<token>")
}

type LogMessenger struct {
	Logger interface{ Info(string, ...any) }
}

func (m LogMessenger) Send(_ context.Context, chatID int64, text string) error {
	m.Logger.Info("notification not sent, no telegram token", "chat_id", chatID, "text_len", len([]rune(text)))
	return nil
}

func (c *AuthClient) Ping(ctx context.Context) error {
	err := httpclient.Call(ctx, c.http, http.MethodPost, c.baseURL+"/internal/telegram/ids", c.token,
		map[string]any{"user_ids": []string{}}, nil, http.StatusOK)
	if err != nil {
		return fmt.Errorf("auth internal lookup: %w", err)
	}
	return nil
}

func (m *TelegramMessenger) Ping(ctx context.Context) error {
	return m.call(ctx, http.MethodGet, "getMe", nil)
}

type NotifyClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewNotifyClient(baseURL, token string) *NotifyClient {
	return &NotifyClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    httpclient.Internal(15 * time.Second),
	}
}

func (c *NotifyClient) Push(ctx context.Context, message PushMessage, userIDs []uuid.UUID) error {
	for start := 0; start < len(userIDs); start += notify.MaxRecipients {
		end := min(start+notify.MaxRecipients, len(userIDs))
		ids := make([]string, 0, end-start)
		for _, id := range userIDs[start:end] {
			ids = append(ids, id.String())
		}
		body := map[string]any{
			"user_ids": ids,
			"kind":     message.Kind,
			"title":    message.Title,
			"body":     message.Body,
			"url":      message.URL,
		}
		if message.CollapseKey != "" {
			body["collapse_key"] = message.CollapseKey
		}
		if message.TTL > 0 {
			body["ttl_seconds"] = int(message.TTL / time.Second)
		}
		if err := c.post(ctx, "/internal/notify", body); err != nil {
			return err
		}
	}
	return nil
}

func (c *NotifyClient) post(ctx context.Context, path string, payload any) error {
	if err := httpclient.Call(ctx, c.http, http.MethodPost, c.baseURL+path, c.token, payload, nil, http.StatusAccepted); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}

func (c *NotifyClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notify healthz answered %d", resp.StatusCode)
	}
	return nil
}
