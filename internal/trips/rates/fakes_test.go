package rates

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lifelog-api/internal/trips"
)

const testKey = "0123456789abcdef01234567"

type fakeStore struct {
	mu        sync.Mutex
	rows      []trips.Snapshot
	nextID    int64
	latestHit int
	insertErr error
	readErr   error
}

func (s *fakeStore) LatestSnapshot(context.Context) (*trips.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestHit++
	if s.readErr != nil {
		return nil, s.readErr
	}
	var best *trips.Snapshot
	for i := range s.rows {
		if best == nil || s.rows[i].ID > best.ID {
			row := s.rows[i]
			best = &row
		}
	}
	return best, nil
}

func (s *fakeStore) SnapshotsAround(_ context.Context, at time.Time) (before, after *trips.Snapshot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return nil, nil, s.readErr
	}
	rows := append([]trips.Snapshot(nil), s.rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	for i := range rows {
		row := rows[i]
		if !row.CreatedAt.After(at) {
			before = &row
		} else if after == nil {
			after = &row
		}
	}
	return before, after, nil
}

func (s *fakeStore) InsertSnapshot(_ context.Context, snapshot *trips.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertErr != nil {
		return s.insertErr
	}
	s.nextID++
	snapshot.ID = s.nextID
	s.rows = append(s.rows, *snapshot)
	return nil
}

func (s *fakeStore) add(id int64, createdAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, trips.Snapshot{ID: id, BaseCurrency: "USD", CreatedAt: createdAt,
		Rates: map[string]trips.Decimal{"USD": trips.MustDecimal("1")}})
	if id > s.nextID {
		s.nextID = id
	}
}

func (s *fakeStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeAPI struct {
	server   *httptest.Server
	calls    atomic.Int64
	failures atomic.Int64
	lastPath atomic.Value
}

const okBody = `{"result":"success","base_code":"USD","time_last_update_utc":"Tue, 15 Sep 2026 00:00:01 +0000",
"conversion_rates":{"USD":1,"AED":3.6725,"THB":33.0608,"JPY":146.2100,"VND":26150.5}}`

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	api := &fakeAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.calls.Add(1)
		api.lastPath.Store(r.URL.Path)
		if api.failures.Load() > 0 {
			api.failures.Add(-1)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(okBody))
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (a *fakeAPI) client() *Client {
	return NewClient(a.server.URL, "6", testKey, a.server.Client())
}

func newTestProvider(store SnapshotStore, client *Client, clock *fakeClock, logs *bytes.Buffer) *Provider {
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return NewProvider(store, client, clock, logger)
}

type recordedWaits struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (r *recordedWaits) list() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.waits...)
}

func installTimer(p *Provider, clock *fakeClock, cancel context.CancelFunc, stopAfter int) *recordedWaits {
	rec := &recordedWaits{}
	p.after = func(d time.Duration) <-chan time.Time {
		rec.mu.Lock()
		rec.waits = append(rec.waits, d)
		n := len(rec.waits)
		rec.mu.Unlock()
		ch := make(chan time.Time, 1)
		if n >= stopAfter {
			cancel()
			return ch
		}
		clock.advance(d)
		ch <- clock.Now()
		return ch
	}
	return rec
}

var errBoom = errors.New("boom")
