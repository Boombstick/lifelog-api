package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

type fakeFCM struct {
	mu       sync.Mutex
	requests []capturedRequest
	answers  []fakeAnswer
}

type capturedRequest struct {
	path   string
	auth   string
	body   map[string]any
	parsed fcmRequest
}

type fakeAnswer struct {
	status  int
	body    string
	headers map[string]string
}

func (f *fakeFCM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	var parsed fcmRequest
	_ = json.Unmarshal(raw, &parsed)

	f.mu.Lock()
	f.requests = append(f.requests, capturedRequest{path: r.URL.Path, auth: r.Header.Get("Authorization"), body: generic, parsed: parsed})
	answer := fakeAnswer{status: http.StatusOK, body: `{"name":"projects/p/messages/1"}`}
	if len(f.answers) > 0 {
		answer, f.answers = f.answers[0], f.answers[1:]
	}
	f.mu.Unlock()

	for k, v := range answer.headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(answer.status)
	_, _ = io.WriteString(w, answer.body)
}

func newTestSender(t *testing.T, fake *fakeFCM) *FCMSender {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-access", TokenType: "Bearer"})
	sender := NewFCMSender("lifelog-test", tokens, server.URL)
	sender.now = func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) }
	return sender
}

func testTarget() Target {
	return Target{ID: uuid.New(), UserID: uuid.New(), Channel: ChannelFCM, Token: "device-token-abcdef"}
}

func testMessage(kind string) Message {
	return Message{
		Kind:        kind,
		Title:       "Стоматолог",
		Body:        "Через 30 мин., в 09:30",
		URL:         "/calendar",
		CollapseKey: "reminder:x",
		ExpiresAt:   time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC),
	}
}

func TestFCMPayloadShape(t *testing.T) {
	t.Parallel()
	fake := &fakeFCM{}
	sender := newTestSender(t, fake)

	if err := sender.Send(context.Background(), testTarget(), testMessage(KindReminder)); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(fake.requests))
	}
	got := fake.requests[0]
	if got.path != "/v1/projects/lifelog-test/messages:send" {
		t.Errorf("path %q", got.path)
	}
	if got.auth != "Bearer test-access" {
		t.Errorf("authorization %q", got.auth)
	}
	m := got.parsed.Message
	if m.Token != "device-token-abcdef" || m.Notification.Title != "Стоматолог" || m.Notification.Body != "Через 30 мин., в 09:30" {
		t.Errorf("message %+v", m)
	}
	if m.Data["url"] != "/calendar" || m.Data["kind"] != KindReminder || len(m.Data) != 2 {
		t.Errorf("data %+v", m.Data)
	}
	a := m.Android
	if a.Priority != "HIGH" || a.TTL != "3600s" || a.CollapseKey != "reminder:x" {
		t.Errorf("android %+v", a)
	}
	if a.Notification.ChannelID != "reminders" || a.Notification.ClickAction != "FLUTTER_NOTIFICATION_CLICK" {
		t.Errorf("android notification %+v", a.Notification)
	}

	message := got.body["message"].(map[string]any)
	android := message["android"].(map[string]any)
	for _, key := range []string{"priority", "ttl", "collapse_key", "notification"} {
		if _, ok := android[key]; !ok {
			t.Errorf("android.%s missing on the wire: %v", key, android)
		}
	}
	if _, ok := android["notification"].(map[string]any)["channel_id"]; !ok {
		t.Errorf("android.notification.channel_id missing")
	}
}

func TestFCMDigestIsNormalPriority(t *testing.T) {
	t.Parallel()
	fake := &fakeFCM{}
	sender := newTestSender(t, fake)
	message := testMessage(KindDigest)
	message.CollapseKey = ""

	if err := sender.Send(context.Background(), testTarget(), message); err != nil {
		t.Fatal(err)
	}
	a := fake.requests[0].parsed.Message.Android
	if a.Priority != "NORMAL" || a.Notification.ChannelID != "digest" {
		t.Errorf("digest android %+v", a)
	}
	android := fake.requests[0].body["message"].(map[string]any)["android"].(map[string]any)
	if _, ok := android["collapse_key"]; ok {
		t.Errorf("empty collapse key must be omitted")
	}
}

func TestFCMErrorClassification(t *testing.T) {
	t.Parallel()

	unregistered := `{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND",
		"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`
	badToken := `{"error":{"code":400,"message":"The registration token is not a valid FCM registration token","status":"INVALID_ARGUMENT",
		"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"INVALID_ARGUMENT"}]}}`
	badField := `{"error":{"code":400,"message":"Invalid value","status":"INVALID_ARGUMENT",
		"details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"message.token","description":"bad"}]}]}}`
	badPayload := `{"error":{"code":400,"message":"Invalid value at 'message.android.ttl'","status":"INVALID_ARGUMENT",
		"details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"message.android.ttl"}]}]}}`
	quota := `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED",
		"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"QUOTA_EXCEEDED"}]}}`

	tests := []struct {
		name      string
		answer    fakeAnswer
		gone      bool
		retry     bool
		wantAfter time.Duration
	}{
		{name: "unregistered", answer: fakeAnswer{status: 404, body: unregistered}, gone: true},
		{name: "invalid token text", answer: fakeAnswer{status: 400, body: badToken}, gone: true},
		{name: "invalid token field", answer: fakeAnswer{status: 400, body: badField}, gone: true},
		{name: "invalid payload keeps device", answer: fakeAnswer{status: 400, body: badPayload}},
		{name: "quota retries with hint", answer: fakeAnswer{status: 429, body: quota, headers: map[string]string{"Retry-After": "120"}}, retry: true, wantAfter: 2 * time.Minute},
		{name: "unavailable retries", answer: fakeAnswer{status: 503, body: `{"error":{"code":503,"status":"UNAVAILABLE"}}`}, retry: true},
		{name: "internal retries", answer: fakeAnswer{status: 500, body: `not json`}, retry: true},
		{name: "unauthorized is config", answer: fakeAnswer{status: 401, body: `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`}},
		{name: "forbidden is config", answer: fakeAnswer{status: 403, body: `{"error":{"code":403,"status":"PERMISSION_DENIED","details":[{"errorCode":"SENDER_ID_MISMATCH"}]}}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeFCM{answers: []fakeAnswer{tt.answer}}
			sender := newTestSender(t, fake)

			err := sender.Send(context.Background(), testTarget(), testMessage(KindReminder))
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.Is(err, ErrTokenGone); got != tt.gone {
				t.Errorf("gone = %v, want %v (%v)", got, tt.gone, err)
			}
			var retry *RetryableError
			if got := errors.As(err, &retry); got != tt.retry {
				t.Errorf("retry = %v, want %v (%v)", got, tt.retry, err)
			}
			if tt.retry && retry.After != tt.wantAfter {
				t.Errorf("retry after %v, want %v", retry.After, tt.wantAfter)
			}
			if strings.Contains(err.Error(), "device-token-abcdef") {
				t.Errorf("error leaks the device token: %v", err)
			}
		})
	}
}

func TestFCMNetworkErrorRetries(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	sender := NewFCMSender("p", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"}), server.URL)
	err := sender.Send(context.Background(), testTarget(), testMessage(KindTest))
	var retry *RetryableError
	if !errors.As(err, &retry) {
		t.Fatalf("a connection failure must be retryable, got %v", err)
	}
}

type failingTokens struct{ err error }

func (f failingTokens) Token() (*oauth2.Token, error) { return nil, f.err }

func TestFCMTokenRefusedIsPermanent(t *testing.T) {
	t.Parallel()
	refused := &oauth2.RetrieveError{Response: &http.Response{StatusCode: 400}}
	sender := NewFCMSender("p", failingTokens{err: refused}, "http://127.0.0.1:1")
	err := sender.Send(context.Background(), testTarget(), testMessage(KindTest))
	var retry *RetryableError
	if err == nil || errors.As(err, &retry) || errors.Is(err, ErrTokenGone) {
		t.Fatalf("a refused key must be a permanent error, got %v", err)
	}

	sender = NewFCMSender("p", failingTokens{err: errors.New("dial tcp: timeout")}, "http://127.0.0.1:1")
	if err := sender.Send(context.Background(), testTarget(), testMessage(KindTest)); !errors.As(err, &retry) {
		t.Fatalf("a token endpoint outage must be retryable, got %v", err)
	}
}

func TestLoadFCMSenderMissingFile(t *testing.T) {
	t.Parallel()
	_, err := LoadFCMSender(t.TempDir()+"/absent.json", "")
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("missing file must be ErrNoCredentials, got %v", err)
	}
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	if got := retryAfter("30", now); got != 30*time.Second {
		t.Errorf("seconds: %v", got)
	}
	if got := retryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); got != 90*time.Second {
		t.Errorf("date: %v", got)
	}
	if got := retryAfter("soon", now); got != 0 {
		t.Errorf("garbage: %v", got)
	}
}
