package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type pushCall struct {
	message PushMessage
	users   []uuid.UUID
}

type fakePusher struct {
	calls []pushCall
	err   error
}

func (f *fakePusher) Push(_ context.Context, message PushMessage, users []uuid.UUID) error {
	f.calls = append(f.calls, pushCall{message: message, users: append([]uuid.UUID(nil), users...)})
	return f.err
}

type failingMessenger struct{ attempts int }

func (f *failingMessenger) Send(context.Context, int64, string) error {
	f.attempts++
	return errors.New("telegram is down")
}

func pushNotifier(now time.Time, repo *fakeNotifierRepo, book fakeBook, messenger Messenger, pusher Pusher) *Notifier {
	n := NewNotifier(repo, &fakeCatalog{}, book, messenger, pusher, quiet())
	n.now = func() time.Time { return now }
	return n
}

func reminderSetup() (uuid.UUID, *fakeNotifierRepo, time.Time) {
	user := uuid.New()
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 30})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Врач", StartsAt: start}}
	return user, repo, start
}

func TestPushReachesUserWithoutTelegram(t *testing.T) {
	t.Parallel()
	user, repo, start := reminderSetup()
	pusher := &fakePusher{}
	messenger := &fakeMessenger{}
	n := pushNotifier(start.Add(-20*time.Minute), repo, fakeBook{}, messenger, pusher)

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 0 {
		t.Errorf("no chat, yet telegram got %d", len(messenger.sent))
	}
	if len(pusher.calls) != 1 {
		t.Fatalf("expected one push call, got %d", len(pusher.calls))
	}
	call := pusher.calls[0]
	if len(call.users) != 1 || call.users[0] != user {
		t.Errorf("recipients %v", call.users)
	}
	m := call.message
	if m.Kind != "calendar.reminder" || !strings.Contains(m.Title, "Врач") || !strings.HasPrefix(m.Body, "Через 30 мин.") || m.URL != "/calendar" {
		t.Errorf("push message %+v", m)
	}
	if m.TTL != 20*time.Minute {
		t.Errorf("a reminder push must live until the entry starts, got %v", m.TTL)
	}

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pusher.calls) != 1 {
		t.Errorf("reminder pushed twice")
	}
}

func TestBothChannelsGetTheMessage(t *testing.T) {
	t.Parallel()
	user, repo, start := reminderSetup()
	pusher := &fakePusher{}
	messenger := &fakeMessenger{}
	n := pushNotifier(start.Add(-20*time.Minute), repo, fakeBook{user: 77}, messenger, pusher)

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 || len(pusher.calls) != 1 {
		t.Fatalf("telegram %d, push %d", len(messenger.sent), len(pusher.calls))
	}
}

func TestFailedSendIsRetriedNextPass(t *testing.T) {
	t.Parallel()
	user, repo, start := reminderSetup()
	messenger := &failingMessenger{}
	n := pushNotifier(start.Add(-20*time.Minute), repo, fakeBook{user: 5}, messenger, nil)

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.released != 1 || len(repo.delivered) != 0 {
		t.Fatalf("a failed send must release its claim: released=%d delivered=%v", repo.released, repo.delivered)
	}
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if messenger.attempts != 2 {
		t.Errorf("the next pass must try again, attempts=%d", messenger.attempts)
	}
}

func TestOneAcceptingChannelIsEnough(t *testing.T) {
	t.Parallel()
	user, repo, start := reminderSetup()
	pusher := &fakePusher{}
	n := pushNotifier(start.Add(-20*time.Minute), repo, fakeBook{user: 5}, &failingMessenger{}, pusher)

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.released != 0 || len(repo.delivered) != 1 {
		t.Fatalf("push took it, the claim must stay: released=%d", repo.released)
	}
}

func TestRejectedPushWithoutTelegramIsRetried(t *testing.T) {
	t.Parallel()
	_, repo, start := reminderSetup()
	pusher := &fakePusher{err: errors.New("notify answered 500")}
	n := pushNotifier(start.Add(-20*time.Minute), repo, fakeBook{}, &fakeMessenger{}, pusher)

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.released != 1 {
		t.Fatalf("a refused push must release the claim, released=%d", repo.released)
	}
}

func TestIdenticalPushesShareOneCall(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	a, b := uuid.New(), uuid.New()
	catalog := &fakeCatalog{events: []CatalogEvent{
		{UserID: a, Kind: KindFilmRelease, FilmID: 1, Title: "Дюна 3", Date: start},
		{UserID: b, Kind: KindFilmRelease, FilmID: 1, Title: "Дюна 3", Date: start},
	}}
	repo := newFakeRepo(
		Settings{UserID: a, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 30},
		Settings{UserID: b, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 30},
	)
	pusher := &fakePusher{}
	n := NewNotifier(repo, catalog, fakeBook{}, &fakeMessenger{}, pusher, quiet())
	n.now = func() time.Time { return start.Add(-20 * time.Minute) }

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pusher.calls) != 1 || len(pusher.calls[0].users) != 2 {
		t.Fatalf("expected one call for both, got %+v", pusher.calls)
	}
}

func TestDigestPushShape(t *testing.T) {
	t.Parallel()
	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Стоматолог", StartsAt: time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)}}
	pusher := &fakePusher{}
	n := pushNotifier(time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), repo, fakeBook{}, &fakeMessenger{}, pusher)

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pusher.calls) != 1 {
		t.Fatalf("expected a digest push, got %d", len(pusher.calls))
	}
	m := pusher.calls[0].message
	if m.Kind != "calendar.digest" || m.Title != "Сегодня" || !strings.Contains(m.Body, "Стоматолог") || m.CollapseKey != "digest" {
		t.Errorf("digest push %+v", m)
	}
}

func TestNotifyClientRequest(t *testing.T) {
	t.Parallel()
	var got map[string]any
	var token string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/notify" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		token = r.Header.Get("X-Internal-Token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"queued":1}`))
	}))
	defer server.Close()

	user := uuid.New()
	client := NewNotifyClient(server.URL+"/", "secret")
	err := client.Push(context.Background(), PushMessage{
		Kind: "calendar.reminder", Title: "Врач", Body: "Через 30 мин., в 18:00",
		URL: "/calendar", CollapseKey: "reminder:event:x", TTL: 20 * time.Minute,
	}, []uuid.UUID{user})
	if err != nil {
		t.Fatal(err)
	}
	if token != "secret" {
		t.Errorf("internal token %q", token)
	}
	ids := got["user_ids"].([]any)
	if len(ids) != 1 || ids[0] != user.String() || got["kind"] != "calendar.reminder" ||
		got["collapse_key"] != "reminder:event:x" || got["ttl_seconds"] != float64(1200) || got["url"] != "/calendar" {
		t.Errorf("body %v", got)
	}
}

func TestNotifyClientRejectsNon202(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	err := NewNotifyClient(server.URL, "wrong").Push(context.Background(), PushMessage{Kind: "test", Title: "x"}, []uuid.UUID{uuid.New()})
	if err == nil {
		t.Fatal("401 must be an error")
	}
}
