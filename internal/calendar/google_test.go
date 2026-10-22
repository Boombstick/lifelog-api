package calendar

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGoogleEventParsing(t *testing.T) {
	page := googleEventsPage{
		TimeZone: "Asia/Bangkok",
		Items: []googleEvent{
			{ID: "timed", Summary: " Встреча ", Start: googleMoment{DateTime: "2026-09-16T10:00:00+07:00"},
				End: googleMoment{DateTime: "2026-09-16T11:00:00+07:00"}, Location: "Кафе", HTMLLink: "https://g/1"},
			{ID: "oneday", Summary: "Отпуск", Start: googleMoment{Date: "2026-09-20"}, End: googleMoment{Date: "2026-09-21"}},
			{ID: "span", Summary: "Поездка", Start: googleMoment{Date: "2026-09-20"}, End: googleMoment{Date: "2026-09-23"}},
			{ID: "cancelled", Status: "cancelled", Start: googleMoment{Date: "2026-09-20"}},
			{ID: "untitled", Start: googleMoment{DateTime: "2026-09-16T12:00:00Z"}},
		},
	}
	declined := googleEvent{ID: "declined", Summary: "Нет", Start: googleMoment{DateTime: "2026-09-16T12:00:00Z"}}
	declined.Attendees = append(declined.Attendees, struct {
		Self           bool   `json:"self"`
		ResponseStatus string `json:"responseStatus"`
	}{Self: true, ResponseStatus: "declined"})
	page.Items = append(page.Items, declined)

	events, err := page.imported()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ImportedEvent{}
	for _, e := range events {
		byID[e.GoogleID] = e
	}
	if len(events) != 4 {
		t.Fatalf("want 4 events (cancelled and declined dropped), got %d", len(events))
	}

	timed := byID["timed"]
	if timed.Title != "Встреча" || timed.AllDay || timed.EndsAt == nil || timed.Link != "https://g/1" {
		t.Errorf("timed event parsed wrong: %+v", timed)
	}
	if !timed.StartsAt.Equal(time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("timed start = %v", timed.StartsAt)
	}

	oneDay := byID["oneday"]
	if !oneDay.AllDay || oneDay.EndsAt != nil {
		t.Errorf("one-day event must be all-day without an end: %+v", oneDay)
	}

	if !oneDay.StartsAt.Equal(time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("all-day start = %v", oneDay.StartsAt)
	}

	span := byID["span"]
	if span.EndsAt == nil || !span.EndsAt.Equal(time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("exclusive end must become the last covered day, got %v", span.EndsAt)
	}
	if byID["untitled"].Title == "" {
		t.Error("an untitled event needs a placeholder title")
	}
}

func TestGoogleAllDayKeepsItsDateEastOfGreenwich(t *testing.T) {
	page := googleEventsPage{
		TimeZone: "Europe/Moscow",
		Items: []googleEvent{{ID: "a", Summary: "День рождения",
			Start: googleMoment{Date: "2026-10-20"}, End: googleMoment{Date: "2026-10-23"}}},
	}
	events, err := page.imported()
	if err != nil {
		t.Fatal(err)
	}
	entry := entryFromImported(events[0])
	if want := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC); !entry.When.Equal(want) {
		t.Errorf("agenda date = %v, want %v", entry.When, want)
	}
	if want := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC); entry.EndsAt == nil || !entry.EndsAt.Equal(want) {
		t.Errorf("agenda end = %v, want %v", entry.EndsAt, want)
	}
}

func TestEmailFromIDToken(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"a@b.c"}`))
	if got := emailFromIDToken("h." + payload + ".s"); got != "a@b.c" {
		t.Errorf("got %q", got)
	}
	if got := emailFromIDToken("garbage"); got != "" {
		t.Errorf("garbage must yield nothing, got %q", got)
	}
}

func testSealer(t *testing.T) *Sealer {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := NewSealer(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return sealer
}

func TestSealer(t *testing.T) {
	sealer := testSealer(t)
	sealed, err := sealer.Seal("refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "refresh-token") {
		t.Fatal("sealed value contains the plain token")
	}
	plain, err := sealer.Open(sealed)
	if err != nil || plain != "refresh-token" {
		t.Fatalf("round trip failed: %q, %v", plain, err)
	}

	sealed[len(sealed)-1] ^= 1
	if _, err := sealer.Open(sealed); err == nil {
		t.Error("a tampered token must not open")
	}
	if _, err := testSealer(t).Open(sealed); err == nil {
		t.Error("another key must not open the token")
	}
	if _, err := NewSealer(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("a short key must be rejected")
	}
}

func TestGoogleEntryRefFitsDeliveryKey(t *testing.T) {
	entry := entryFromImported(ImportedEvent{GoogleID: strings.Repeat("x", 1024)})
	if ref := entry.Ref(); len(ref) > 128 || !strings.HasPrefix(ref, "google:") {
		t.Errorf("ref %q (len %d) does not fit the delivery key", ref, len(ref))
	}
}

type fakeGoogleRepo struct {
	mu       sync.Mutex
	states   map[string]GoogleState
	links    map[uuid.UUID]GoogleLink
	imported map[uuid.UUID][]ImportedEvent
}

func newFakeGoogleRepo() *fakeGoogleRepo {
	return &fakeGoogleRepo{
		states:   map[string]GoogleState{},
		links:    map[uuid.UUID]GoogleLink{},
		imported: map[uuid.UUID][]ImportedEvent{},
	}
}

func (f *fakeGoogleRepo) SaveGoogleState(_ context.Context, s *GoogleState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[s.Hash] = *s
	return nil
}
func (f *fakeGoogleRepo) TakeGoogleState(_ context.Context, hash string, now time.Time) (*GoogleState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.states[hash]
	delete(f.states, hash)
	if !ok || !s.ExpiresAt.After(now) {
		return nil, ErrGoogleBadState
	}
	return &s, nil
}
func (f *fakeGoogleRepo) ForgetGoogleStates(context.Context, time.Time) error { return nil }
func (f *fakeGoogleRepo) GoogleLink(_ context.Context, userID uuid.UUID) (*GoogleLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	link, ok := f.links[userID]
	if !ok {
		return nil, ErrGoogleNotLinked
	}
	return &link, nil
}
func (f *fakeGoogleRepo) SaveGoogleLink(_ context.Context, link *GoogleLink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[link.UserID] = *link
	return nil
}
func (f *fakeGoogleRepo) ActiveGoogleLinks(context.Context) ([]GoogleLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []GoogleLink
	for _, link := range f.links {
		if !link.Revoked {
			out = append(out, link)
		}
	}
	return out, nil
}
func (f *fakeGoogleRepo) MarkGoogleSync(_ context.Context, userID uuid.UUID, at *time.Time, lastError string, revoked bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	link := f.links[userID]
	link.LastSyncAt, link.LastError, link.Revoked = at, lastError, revoked
	f.links[userID] = link
	return nil
}
func (f *fakeGoogleRepo) DeleteGoogleLink(_ context.Context, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.links, userID)
	delete(f.imported, userID)
	return nil
}
func (f *fakeGoogleRepo) ReplaceImported(_ context.Context, userID uuid.UUID, events []ImportedEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imported[userID] = events
	return nil
}

type fakeGoogle struct {
	grant   GoogleGrant
	events  []ImportedEvent
	err     error
	revoked []string

	from time.Time
	to   time.Time
}

func (f *fakeGoogle) AuthURL(state string) string { return "https://google.test/auth?state=" + state }
func (f *fakeGoogle) Exchange(context.Context, string) (*GoogleGrant, error) {
	grant := f.grant
	return &grant, nil
}
func (f *fakeGoogle) Events(_ context.Context, _ string, from, to time.Time) ([]ImportedEvent, error) {
	f.from, f.to = from, to
	return f.events, f.err
}
func (f *fakeGoogle) Revoke(_ context.Context, token string) error {
	f.revoked = append(f.revoked, token)
	return nil
}

func stateFrom(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("state")
}

func TestGoogleImportFlow(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGoogleRepo()
	google := &fakeGoogle{
		grant:  GoogleGrant{RefreshToken: "rt", Email: "me@gmail.com", Scopes: []string{"openid", GoogleEventsScope}},
		events: []ImportedEvent{{GoogleID: "a", Title: "A"}, {GoogleID: "b", Title: "B"}},
	}
	imp := NewGoogleImport(repo, google, testSealer(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	user := uuid.New()

	authURL, err := imp.Connect(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	state := stateFrom(t, authURL)

	count, syncErr, err := imp.Complete(ctx, state, "code")
	if err != nil || syncErr != nil || count != 2 {
		t.Fatalf("complete: count=%d syncErr=%v err=%v", count, syncErr, err)
	}
	if got := repo.imported[user]; len(got) != 2 || got[0].UserID != user {
		t.Fatalf("imported events not stored for the user: %+v", got)
	}
	if string(repo.links[user].RefreshToken) == "rt" {
		t.Fatal("refresh token stored in the clear")
	}

	if _, _, err := imp.Complete(ctx, state, "code"); !errors.Is(err, ErrGoogleBadState) {
		t.Errorf("reused state: want ErrGoogleBadState, got %v", err)
	}

	status, _ := imp.Status(ctx, user)
	if !status.Enabled || !status.Connected || status.Email != "me@gmail.com" || status.LastSyncAt == nil {
		t.Errorf("status: %+v", status)
	}

	google.err = ErrGoogleRevoked
	if _, err := imp.Sync(ctx, user); !errors.Is(err, ErrGoogleRevoked) {
		t.Fatalf("want ErrGoogleRevoked, got %v", err)
	}
	if len(repo.imported[user]) != 0 || !repo.links[user].Revoked {
		t.Errorf("revocation must wipe events and flag the link: %+v", repo.links[user])
	}
	if links, _ := repo.ActiveGoogleLinks(ctx); len(links) != 0 {
		t.Error("a revoked link must drop out of the scheduled pass")
	}

	if err := imp.Disconnect(ctx, user); err != nil {
		t.Fatal(err)
	}
	if len(google.revoked) != 1 || google.revoked[0] != "rt" {
		t.Errorf("disconnect must revoke the grant at Google: %v", google.revoked)
	}
	if _, err := imp.Status(ctx, user); err != nil {
		t.Fatal(err)
	}
	if status, _ := imp.Status(ctx, user); status.Connected {
		t.Error("still connected after disconnect")
	}
}

func TestGoogleImportRejectsGrantWithoutCalendarScope(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGoogleRepo()
	google := &fakeGoogle{grant: GoogleGrant{RefreshToken: "rt", Scopes: []string{"openid", "email"}}}
	imp := NewGoogleImport(repo, google, testSealer(t), slog.New(slog.NewTextHandler(io.Discard, nil)))

	authURL, _ := imp.Connect(ctx, uuid.New())
	if _, _, err := imp.Complete(ctx, stateFrom(t, authURL), "code"); !errors.Is(err, ErrGoogleScopeMissing) {
		t.Fatalf("want ErrGoogleScopeMissing, got %v", err)
	}
	if len(repo.links) != 0 {
		t.Error("a useless grant must not be stored")
	}
	if len(google.revoked) != 1 {
		t.Error("a useless grant should be handed back to Google")
	}
}

func TestGoogleImportExpiredState(t *testing.T) {
	ctx := context.Background()
	imp := NewGoogleImport(newFakeGoogleRepo(), &fakeGoogle{}, testSealer(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	authURL, _ := imp.Connect(ctx, uuid.New())
	imp.now = func() time.Time { return time.Now().UTC().Add(stateTTL + time.Minute) }
	if _, _, err := imp.Complete(ctx, stateFrom(t, authURL), "code"); !errors.Is(err, ErrGoogleBadState) {
		t.Fatalf("want ErrGoogleBadState, got %v", err)
	}
}

func TestNilGoogleImportIsDisabled(t *testing.T) {
	var imp *GoogleImport
	if _, err := imp.Connect(context.Background(), uuid.New()); !errors.Is(err, ErrGoogleDisabled) {
		t.Errorf("want ErrGoogleDisabled, got %v", err)
	}
	if status, err := imp.Status(context.Background(), uuid.New()); err != nil || status.Enabled {
		t.Errorf("disabled status: %+v, %v", status, err)
	}
	if err := imp.SyncAll(context.Background()); err != nil {
		t.Errorf("scheduled pass on a disabled import must be a no-op, got %v", err)
	}
}

func TestGoogleClientEvents(t *testing.T) {
	var refused bool
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if refused || r.Form.Get("refresh_token") != "rt" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("singleEvents") != "true" {
			t.Error("recurring events must be expanded")
		}
		page := googleEventsPage{TimeZone: "UTC"}
		if r.URL.Query().Get("pageToken") == "" {
			page.NextPageToken = "p2"
			page.Items = []googleEvent{{ID: "1", Summary: "one", Start: googleMoment{Date: "2026-09-16"}}}
		} else {
			page.Items = []googleEvent{{ID: "2", Summary: "two", Start: googleMoment{Date: "2026-09-17"}}}
		}
		_ = json.NewEncoder(w).Encode(page)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewGoogleClient(GoogleConfig{
		ClientID: "id", ClientSecret: "secret", RedirectURL: "https://x/cb",
		AuthURL: server.URL + "/auth", TokenURL: server.URL + "/token", APIURL: server.URL,
	})

	now := time.Now()
	events, err := client.Events(context.Background(), "rt", now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].GoogleID != "1" || events[1].GoogleID != "2" {
		t.Fatalf("pagination: %+v", events)
	}

	refused = true
	if _, err := client.Events(context.Background(), "rt", now, now.Add(time.Hour)); !errors.Is(err, ErrGoogleRevoked) {
		t.Fatalf("refused refresh token: want ErrGoogleRevoked, got %v", err)
	}

	authURL, _ := url.Parse(client.AuthURL("s"))
	query := authURL.Query()
	if query.Get("access_type") != "offline" || query.Get("prompt") != "consent" ||
		!strings.Contains(query.Get("scope"), GoogleEventsScope) {
		t.Errorf("consent URL lacks offline access or the scope: %s", authURL)
	}
}

func TestGoogleSyncCopiesAWindowAroundNow(t *testing.T) {
	ctx := context.Background()
	repo := newFakeGoogleRepo()
	google := &fakeGoogle{grant: GoogleGrant{RefreshToken: "rt", Scopes: []string{GoogleEventsScope}}}
	imp := NewGoogleImport(repo, google, testSealer(t), slog.New(slog.NewTextHandler(io.Discard, nil)))

	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	imp.now = func() time.Time { return now }

	authURL, err := imp.Connect(ctx, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, syncErr, err := imp.Complete(ctx, stateFrom(t, authURL), "code"); err != nil || syncErr != nil {
		t.Fatalf("complete: syncErr=%v err=%v", syncErr, err)
	}

	if !google.from.Equal(now.Add(-importBack)) {
		t.Errorf("window starts at %v, want %v — the past must be included", google.from, now.Add(-importBack))
	}
	if !google.to.Equal(now.Add(importAhead)) {
		t.Errorf("window ends at %v, want %v", google.to, now.Add(importAhead))
	}
	if !google.from.Before(now) || !google.to.After(now) {
		t.Errorf("the window must straddle now: %v … %v", google.from, google.to)
	}
}
