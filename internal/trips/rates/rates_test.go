package rates

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"lifelog-api/internal/trips"
)

var t0 = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func gaugeValue(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	if err := snapshotTimestamp.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetGauge().GetValue()
}

func TestLatestLoadsOnceAndCaches(t *testing.T) {
	store := &fakeStore{}
	store.add(109, t0.Add(-12*time.Hour))
	store.add(110, t0)
	p := NewProvider(store, nil, &fakeClock{now: t0}, nil)

	for i := 0; i < 3; i++ {
		got, err := p.Latest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.ID != 110 {
			t.Fatalf("got %+v, want id 110", got)
		}
	}
	if store.latestHit != 1 {
		t.Errorf("store read %d times, want 1", store.latestHit)
	}
	if want := float64(t0.Unix()); gaugeValue(t) != want {
		t.Errorf("gauge = %v, want %v", gaugeValue(t), want)
	}
}

func TestLatestEmptyIsNilAndNotCached(t *testing.T) {
	store := &fakeStore{}
	p := NewProvider(store, nil, &fakeClock{now: t0}, nil)

	got, err := p.Latest(context.Background())
	if err != nil || got != nil {
		t.Fatalf("got %+v, %v; want nil, nil", got, err)
	}

	store.add(1, t0)
	got, err = p.Latest(context.Background())
	if err != nil || got == nil || got.ID != 1 {
		t.Fatalf("got %+v, %v; want id 1", got, err)
	}
}

func TestLatestStoreError(t *testing.T) {
	store := &fakeStore{readErr: errBoom}
	p := NewProvider(store, nil, &fakeClock{now: t0}, nil)

	if _, err := p.Latest(context.Background()); !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}

func TestRefreshStoresSnapshotAndReplacesCache(t *testing.T) {
	api := newFakeAPI(t)
	store := &fakeStore{}
	store.add(110, t0.Add(-48*time.Hour))
	clock := &fakeClock{now: t0.Add(time.Nanosecond * 1500)}
	var logs bytes.Buffer
	p := newTestProvider(store, api.client(), clock, &logs)

	if _, err := p.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	latest, _ := p.Latest(context.Background())
	if latest.ID != 111 || latest.BaseCurrency != "USD" || !latest.CreatedAt.Equal(clock.Now()) {
		t.Errorf("latest = %+v", latest)
	}
	if latest.Rates["THB"].String() != "33.0608" {
		t.Errorf("THB = %s", latest.Rates["THB"])
	}
	if store.latestHit != 1 {
		t.Errorf("store read %d times after refresh, want 1", store.latestHit)
	}
	if store.rows[1].ID != 111 {
		t.Errorf("stored row = %+v", store.rows[1])
	}
	if strings.Contains(logs.String(), testKey) {
		t.Errorf("logs leak the key: %s", logs.String())
	}
}

func TestRefreshFailureKeepsCache(t *testing.T) {
	cases := map[string]func(*fakeAPI, *fakeStore){
		"api down":    func(a *fakeAPI, _ *fakeStore) { a.failures.Store(1) },
		"insert fail": func(_ *fakeAPI, s *fakeStore) { s.insertErr = errBoom },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t)
			store := &fakeStore{}
			store.add(7, t0)
			p := NewProvider(store, api.client(), &fakeClock{now: t0.Add(time.Hour)}, nil)
			if _, err := p.Latest(context.Background()); err != nil {
				t.Fatal(err)
			}
			breakIt(api, store)

			err := p.Refresh(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("error leaks the key: %q", err)
			}
			if got, _ := p.Latest(context.Background()); got.ID != 7 {
				t.Errorf("cache changed to %+v", got)
			}
		})
	}
}

func TestClosest(t *testing.T) {
	before := t0
	after := t0.Add(12 * time.Hour)
	mid := t0.Add(6 * time.Hour)

	cases := []struct {
		name   string
		rows   map[int64]time.Time
		at     time.Time
		wantID int64
	}{
		{"no snapshots", nil, mid, 0},
		{"only before", map[int64]time.Time{1: before}, after.Add(time.Hour), 1},
		{"only after", map[int64]time.Time{2: after}, before.Add(-time.Hour), 2},
		{"tie goes to before", map[int64]time.Time{1: before, 2: after}, mid, 1},
		{"after is closer", map[int64]time.Time{1: before, 2: after}, mid.Add(time.Microsecond), 2},
		{"before is closer", map[int64]time.Time{1: before, 2: after}, mid.Add(-time.Microsecond), 1},
		{"exactly at a snapshot counts as before", map[int64]time.Time{1: before, 2: after}, after, 2},
		{"exactly at the first", map[int64]time.Time{1: before, 2: after}, before, 1},
		{"offset input is compared as an instant", map[int64]time.Time{1: before, 2: after},
			mid.Add(time.Minute).In(time.FixedZone("ICT", 7*3600)), 2},
		{"id order does not matter, created_at does", map[int64]time.Time{5: before, 3: after}, before.Add(time.Hour), 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			for id, at := range tc.rows {
				store.add(id, at)
			}
			p := NewProvider(store, nil, &fakeClock{now: t0}, nil)

			got, err := p.Closest(context.Background(), tc.at)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.wantID == 0 && got != nil:
				t.Errorf("got %+v, want nil", got)
			case tc.wantID != 0 && (got == nil || got.ID != tc.wantID):
				t.Errorf("got %+v, want id %d", got, tc.wantID)
			}
		})
	}
}

func TestPickClosestUsesUTCInstant(t *testing.T) {
	b := &trips.Snapshot{ID: 1, CreatedAt: t0}
	a := &trips.Snapshot{ID: 2, CreatedAt: t0.Add(2 * time.Hour)}
	at := t0.Add(time.Hour).In(time.FixedZone("NY", -4*3600))
	if got := pickClosest(at, b, a); got.ID != 1 {
		t.Errorf("tie picked %d, want 1", got.ID)
	}
}

func TestClosestStoreError(t *testing.T) {
	p := NewProvider(&fakeStore{readErr: errBoom}, nil, &fakeClock{now: t0}, nil)
	if _, err := p.Closest(context.Background(), t0); !errors.Is(err, errBoom) {
		t.Errorf("err = %v, want errBoom", err)
	}
}
