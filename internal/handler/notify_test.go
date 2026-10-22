package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog-api/internal/notify"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type notifyRepoStub struct{ found bool }

func (notifyRepoStub) UpsertDevice(context.Context, *notify.Device) error    { return nil }
func (notifyRepoStub) DeleteDevice(context.Context, uuid.UUID, string) error { return nil }
func (s notifyRepoStub) SetDeviceEnabled(context.Context, uuid.UUID, string, bool) (bool, error) {
	return s.found, nil
}
func (notifyRepoStub) Devices(context.Context, uuid.UUID) ([]notify.Device, error) { return nil, nil }
func (notifyRepoStub) PushEnabled(context.Context, uuid.UUID) (bool, error)        { return true, nil }
func (notifyRepoStub) SetPushEnabled(context.Context, uuid.UUID, bool) error       { return nil }
func (notifyRepoStub) Enqueue(context.Context, []uuid.UUID, []string, []byte) (int, error) {
	return 1, nil
}

func notifyEngine(t *testing.T, repo notify.Repository) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	user := uuid.New()
	auth := func(c *gin.Context) {
		c.Set("auth_user_id", user)
		c.Next()
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	NewNotifyHandler(logger, notify.NewService(repo, nil)).Register(engine, auth, RequireInternalToken("secret"))
	return engine
}

func call(engine *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestNotifyRoutes(t *testing.T) {
	engine := notifyEngine(t, notifyRepoStub{})
	device := "/notify/devices/" + uuid.NewString()

	tests := []struct {
		name, method, path, body string
		headers                  map[string]string
		want                     int
	}{
		{"register", http.MethodPut, device, `{"channel":"fcm","token":"t","platform":"android"}`, nil, http.StatusNoContent},
		{"register bad channel", http.MethodPut, device, `{"channel":"sms","token":"t"}`, nil, http.StatusBadRequest},
		{"register bad id", http.MethodPut, "/notify/devices/phone", `{"channel":"fcm","token":"t"}`, nil, http.StatusBadRequest},
		{"patch without enabled", http.MethodPatch, device, `{}`, nil, http.StatusBadRequest},
		{"patch foreign device", http.MethodPatch, device, `{"enabled":false}`, nil, http.StatusNotFound},
		{"delete absent", http.MethodDelete, device, ``, nil, http.StatusNoContent},
		{"settings", http.MethodGet, "/notify/settings", ``, nil, http.StatusOK},
		{"settings without value", http.MethodPut, "/notify/settings", `{}`, nil, http.StatusBadRequest},
		{"settings off", http.MethodPut, "/notify/settings", `{"push_enabled":false}`, nil, http.StatusNoContent},
		{"internal without token", http.MethodPost, "/internal/notify", `{}`, nil, http.StatusUnauthorized},
		{"internal bad kind", http.MethodPost, "/internal/notify", `{"user_ids":["` + uuid.NewString() + `"],"kind":"x","title":"t"}`,
			map[string]string{"X-Internal-Token": "secret"}, http.StatusBadRequest},
		{"internal ok", http.MethodPost, "/internal/notify", `{"user_ids":["` + uuid.NewString() + `"],"kind":"calendar.digest","title":"Сегодня","url":"/calendar"}`,
			map[string]string{"X-Internal-Token": "secret"}, http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := call(engine, tt.method, tt.path, tt.body, tt.headers)
			if rec.Code != tt.want {
				t.Fatalf("%s %s: %d %s, want %d", tt.method, tt.path, rec.Code, rec.Body, tt.want)
			}
		})
	}
}

func TestNotifyTestIsRateLimited(t *testing.T) {
	engine := notifyEngine(t, notifyRepoStub{})
	for i := range 5 {
		if rec := call(engine, http.MethodPost, "/notify/test", ``, nil); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"devices":1`) {
			t.Fatalf("test %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec := call(engine, http.MethodPost, "/notify/test", ``, nil)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Слишком часто") {
		t.Fatalf("sixth test: %d %s", rec.Code, rec.Body)
	}
}

func TestNotifyWebPushRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := func(c *gin.Context) {
		c.Set("auth_user_id", uuid.New())
		c.Next()
	}
	build := func(enabled bool) *gin.Engine {
		engine := gin.New()
		service := notify.NewService(notifyRepoStub{}, nil)
		if enabled {
			service.EnableWebPush("BKEY", notify.EndpointGuard{})
		}
		NewNotifyHandler(logger, service).Register(engine, auth, RequireInternalToken("secret"))
		return engine
	}
	off, on := build(false), build(true)
	device := "/notify/devices/" + uuid.NewString()

	subscription := `{"channel":"webpush","endpoint":"%s","keys":{"p256dh":"BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4","auth":"BTBZMqHH6r4Tts7J_aSIgg"},"platform":"web","label":"Chrome"}`
	good := fmt.Sprintf(subscription, "https://fcm.googleapis.com/wp/abc")

	tests := []struct {
		name         string
		engine       *gin.Engine
		method, path string
		body         string
		want         int
		contains     string
	}{
		{"key when off", off, http.MethodGet, "/notify/vapid-key", ``, http.StatusServiceUnavailable, "не настроены"},
		{"key when on", on, http.MethodGet, "/notify/vapid-key", ``, http.StatusOK, `{"public_key":"BKEY"}`},
		{"register when off", off, http.MethodPut, device, good, http.StatusServiceUnavailable, "не настроены"},
		{"register", on, http.MethodPut, device, good, http.StatusNoContent, ""},
		{"foreign host", on, http.MethodPut, device, fmt.Sprintf(subscription, "https://169.254.169.254/latest"), http.StatusBadRequest, "адрес подписки"},
		{"bad keys", on, http.MethodPut, device, `{"channel":"webpush","endpoint":"https://fcm.googleapis.com/wp/abc","keys":{"p256dh":"x","auth":"y"}}`, http.StatusBadRequest, "ключи подписки"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := call(tt.engine, tt.method, tt.path, tt.body, nil)
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.contains) {
				t.Fatalf("%d %s, want %d containing %q", rec.Code, rec.Body, tt.want, tt.contains)
			}
		})
	}
}
