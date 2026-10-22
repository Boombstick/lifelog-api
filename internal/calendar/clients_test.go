package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const secretBotToken = "123456:AAH-secret-bot-token"

func TestTelegramErrorsNeverCarryToken(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()

	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("relay failed for " + r.URL.Path))
	}))
	defer echo.Close()

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()

	cases := []struct {
		name    string
		baseURL string
		timeout time.Duration
	}{
		{"connection refused", downURL, time.Second},
		{"non-200 echoing the path", echo.URL, time.Second},
		{"deadline exceeded", slow.URL, 50 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewTelegramMessenger(secretBotToken, tc.baseURL, "")
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			for name, call := range map[string]func() error{
				"Send": func() error { return m.Send(ctx, 42, "hi") },
				"Ping": func() error { return m.Ping(ctx) },
			} {
				err := call()
				if err == nil {
					t.Fatalf("%s: expected an error", name)
				}
				if strings.Contains(err.Error(), secretBotToken) || strings.Contains(err.Error(), "AAH-secret") {
					t.Fatalf("%s: token leaked into error: %q", name, err)
				}
			}
		})
	}
}

func TestTelegramSendHitsBotPath(t *testing.T) {
	var gotPath, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotSecret = r.URL.Path, r.Header.Get("X-Relay-Secret")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	m := NewTelegramMessenger(secretBotToken, srv.URL, "relay")
	if err := m.Send(context.Background(), 1, "x"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/bot"+secretBotToken+"/sendMessage" || gotSecret != "relay" {
		t.Fatalf("path %q secret %q", gotPath, gotSecret)
	}
}
