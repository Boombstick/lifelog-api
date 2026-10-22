package visits

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitizePathDropsIdsAndTokens(t *testing.T) {
	cases := map[string]string{
		"/api/v1/films/4938":                                     "/api/v1/films/:id",
		"/api/v1/films/4938/seasons/2":                           "/api/v1/films/:id/seasons/:id",
		"/api/trips/photos/bea91e68-e3ae-4f05-ac9e-cc20d4164ab5": "/api/trips/photos/:id",
		"/s/Zm9vYmFyYmF6cXV4cXV1eHF1dXg":                         "/s/:token",
		"/api/v1/search/films":                                   "/api/v1/search/films",
		"/notes/tags":                                            "/notes/tags",
	}
	for in, want := range cases {
		if got := SanitizePath(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

type memorySink struct{ events []Event }

func (m *memorySink) Record(_ context.Context, events []Event) error {
	m.events = append(m.events, events...)
	return nil
}

func TestWrapRecordsOnlyOutsideRequests(t *testing.T) {
	sink := &memorySink{}
	recorder := NewRecorder("films", sink, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
		}
	}), recorder)

	send := func(path, ip, ua string) {
		r := httptest.NewRequest(http.MethodGet, path+"?q=secret", nil)
		if ip != "" {
			r.Header.Set("X-Real-IP", ip)
		}
		r.Header.Set("User-Agent", ua)
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	send("/api/v1/films/12", "1.2.3.4", "Mozilla")
	send("/missing", "1.2.3.4", "Mozilla")
	send("/api/v1/films", "", "Go-http-client")
	send("/metrics", "1.2.3.4", "Mozilla")
	send("/internal/follows/x", "1.2.3.4", "Mozilla")
	send("/", "1.2.3.4", "Blackbox Exporter/0.25.0")
	recorder.Flush(context.Background())

	if len(sink.events) != 2 {
		t.Fatalf("recorded %d events, want 2: %+v", len(sink.events), sink.events)
	}
	first, second := sink.events[0], sink.events[1]
	if first.Path != "/api/v1/films/:id" || first.Status != 200 || first.Service != "films" || first.IP != "1.2.3.4" {
		t.Errorf("first event %+v", first)
	}
	if second.Status != http.StatusNotFound || strings.Contains(second.Path, "secret") {
		t.Errorf("second event %+v", second)
	}
}

func TestWrapWithoutRecorderIsTransparent(t *testing.T) {
	next := http.NotFoundHandler()
	if Wrap(next, nil) == nil {
		t.Fatal("nil handler")
	}
}

type fakeRepo struct {
	salts    map[string][]byte
	days     map[visitKey]*VisitorDay
	requests []StoredRequest
	trimmed  []visitKey
	pruned   [2]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{salts: map[string][]byte{}, days: map[visitKey]*VisitorDay{}}
}

func (f *fakeRepo) Salt(_ context.Context, day string) ([]byte, error) {
	if s, ok := f.salts[day]; ok {
		return s, nil
	}
	f.salts[day] = []byte("salt-" + day)
	return f.salts[day], nil
}

func (f *fakeRepo) Touch(_ context.Context, v VisitorDay) (int, error) {
	key := visitKey{v.Day, v.Visitor}
	if old, ok := f.days[key]; ok {
		old.Requests += v.Requests
		return old.Requests, nil
	}
	copy := v
	f.days[key] = &copy
	return v.Requests, nil
}

func (f *fakeRepo) AddRequests(_ context.Context, r []StoredRequest) error {
	f.requests = append(f.requests, r...)
	return nil
}

func (f *fakeRepo) TrimRequests(_ context.Context, day, visitor string, _ int) error {
	f.trimmed = append(f.trimmed, visitKey{day, visitor})
	return nil
}

func (f *fakeRepo) Prune(_ context.Context, salts, days string) error {
	f.pruned = [2]string{salts, days}
	return nil
}

func (f *fakeRepo) Days(context.Context, string) ([]DayCount, error)       { return nil, nil }
func (f *fakeRepo) Visitors(context.Context, string) ([]VisitorDay, error) { return nil, nil }
func (f *fakeRepo) Requests(context.Context, string, string) ([]StoredRequest, error) {
	return nil, errors.New("unused")
}

func TestLedgerNeverStoresTheAddress(t *testing.T) {
	repo := newFakeRepo()
	ledger := NewLedger(repo)
	now := time.Now().UTC()
	err := ledger.Record(context.Background(), []Event{
		{IP: "203.0.113.7", At: now, Service: "films", Method: "GET", Path: "/a", Status: 200, UserAgent: "UA1"},
		{IP: "203.0.113.7", At: now.Add(time.Second), Service: "trips", Method: "GET", Path: "/b", Status: 200, UserAgent: "UA2"},
		{IP: "198.51.100.1", At: now, Service: "films", Method: "GET", Path: "/a", Status: 200},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.days) != 2 || len(repo.requests) != 3 {
		t.Fatalf("days %d, requests %d", len(repo.days), len(repo.requests))
	}
	for key, day := range repo.days {
		if strings.Contains(key.visitor, "203") || len(key.visitor) != 16 {
			t.Errorf("visitor id %q", key.visitor)
		}
		if day.Requests == 2 && day.UserAgent != "UA2" {
			t.Errorf("user agent %q, want the latest", day.UserAgent)
		}
	}
	for _, r := range repo.requests {
		if strings.Contains(r.Visitor, ".") {
			t.Errorf("address leaked into %+v", r)
		}
	}
}

func TestVisitorIDDependsOnTheDaySalt(t *testing.T) {
	a := VisitorID([]byte("monday"), "203.0.113.7")
	if a != VisitorID([]byte("monday"), "203.0.113.7") {
		t.Fatal("same day, same address, different ids")
	}
	if a == VisitorID([]byte("tuesday"), "203.0.113.7") {
		t.Fatal("ids match across days")
	}
	if a == VisitorID([]byte("monday"), "203.0.113.8") {
		t.Fatal("different addresses, same id")
	}
}

func TestLedgerRefusesDaysWhoseSaltIsGone(t *testing.T) {
	repo := newFakeRepo()
	err := NewLedger(repo).Record(context.Background(), []Event{{IP: "1.1.1.1", At: time.Now().UTC().AddDate(0, 0, -3)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.salts) != 0 || len(repo.requests) != 0 {
		t.Fatalf("an old day got a salt or a request: %v %v", repo.salts, repo.requests)
	}
}

func TestLedgerTrimsPastTheKeepLimit(t *testing.T) {
	repo := newFakeRepo()
	ledger := NewLedger(repo)
	now := time.Now().UTC()
	events := make([]Event, KeepRequests+1)
	for i := range events {
		events[i] = Event{IP: "1.1.1.1", At: now}
	}
	if err := ledger.Record(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	if len(repo.trimmed) != 1 {
		t.Fatalf("trimmed %v", repo.trimmed)
	}
}

func TestPruneDropsYesterdaysSaltAfterTheFirstHour(t *testing.T) {
	repo := newFakeRepo()
	at := time.Date(2026, 10, 22, 0, 30, 0, 0, time.UTC)
	if err := NewLedger(repo).Prune(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if repo.pruned[0] != "2026-10-21" {
		t.Errorf("at 00:30 salts before %s, want yesterday kept", repo.pruned[0])
	}
	if err := NewLedger(repo).Prune(context.Background(), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if repo.pruned[0] != "2026-10-22" || repo.pruned[1] != "2026-09-22" {
		t.Errorf("at 01:30 pruned %v", repo.pruned)
	}
}
