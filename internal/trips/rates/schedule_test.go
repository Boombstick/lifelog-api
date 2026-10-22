package rates

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNextBoundary(t *testing.T) {
	day := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		now   time.Time
		every time.Duration
		want  time.Time
	}{
		{day.Add(11*time.Hour + 55*time.Minute), 12 * time.Hour, day.Add(12 * time.Hour)},
		{day.Add(12 * time.Hour), 12 * time.Hour, day.Add(24 * time.Hour)},
		{day, 12 * time.Hour, day.Add(12 * time.Hour)},
		{day.Add(23*time.Hour + 59*time.Minute), 12 * time.Hour, day.Add(24 * time.Hour)},
		{day.Add(90 * time.Minute), time.Hour, day.Add(2 * time.Hour)},

		{day.Add(11*time.Hour + 30*time.Minute).In(time.FixedZone("ICT", 7*3600)), 12 * time.Hour, day.Add(12 * time.Hour)},
	}
	for _, tc := range cases {
		if got := nextBoundary(tc.now, tc.every); !got.Equal(tc.want) {
			t.Errorf("nextBoundary(%s, %s) = %s, want %s", tc.now, tc.every, got, tc.want)
		}
	}
}

func TestRunRefreshesAtStartThenAtUTCBoundaries(t *testing.T) {
	api := newFakeAPI(t)
	store := &fakeStore{}
	clock := &fakeClock{now: time.Date(2026, 9, 16, 11, 55, 0, 0, time.UTC)}
	var logs bytes.Buffer
	p := newTestProvider(store, api.client(), clock, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := installTimer(p, clock, cancel, 4)

	if err := p.Run(ctx, 12*time.Hour); err != nil {
		t.Fatalf("Run returned %v", err)
	}

	if got := store.count(); got != 4 {
		t.Errorf("stored %d snapshots, want 4", got)
	}
	want := []time.Duration{5 * time.Minute, 12 * time.Hour, 12 * time.Hour, 12 * time.Hour}
	if got := waits.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	for i, row := range store.rows {
		if i > 0 && (row.CreatedAt.Minute() != 0 || row.CreatedAt.Hour()%12 != 0) {
			t.Errorf("snapshot %d created at %s, want a 00/12 UTC boundary", row.ID, row.CreatedAt)
		}
	}
}

func TestRunDefaultsPeriod(t *testing.T) {
	api := newFakeAPI(t)
	clock := &fakeClock{now: time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC)}
	p := newTestProvider(&fakeStore{}, api.client(), clock, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := installTimer(p, clock, cancel, 1)

	_ = p.Run(ctx, 0)

	if got := waits.list(); len(got) != 1 || got[0] != 9*time.Hour {
		t.Errorf("waits = %v, want [9h]", got)
	}
}

func TestRunRetriesFailedRefresh(t *testing.T) {
	api := newFakeAPI(t)
	api.failures.Store(2)
	store := &fakeStore{}
	clock := &fakeClock{now: time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)}
	var logs bytes.Buffer
	p := newTestProvider(store, api.client(), clock, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := installTimer(p, clock, cancel, 3)

	_ = p.Run(ctx, 12*time.Hour)

	if api.calls.Load() != 3 || store.count() != 1 {
		t.Errorf("calls = %d, stored = %d; want 3 and 1", api.calls.Load(), store.count())
	}
	want := []time.Duration{15 * time.Minute, 15 * time.Minute, 11*time.Hour - 30*time.Minute}
	if got := waits.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	if strings.Contains(logs.String(), testKey) {
		t.Errorf("logs leak the key: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "retrying") {
		t.Errorf("retry not logged: %s", logs.String())
	}
}

func TestRunGivesUpAfterMaxRetries(t *testing.T) {
	api := newFakeAPI(t)
	api.failures.Store(100)
	store := &fakeStore{}
	clock := &fakeClock{now: time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)}
	var logs bytes.Buffer
	p := newTestProvider(store, api.client(), clock, &logs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := installTimer(p, clock, cancel, 4)

	if err := p.Run(ctx, 12*time.Hour); err != nil {
		t.Fatalf("Run returned %v; a failed refresh must not end the job", err)
	}

	if api.calls.Load() != 4 {
		t.Errorf("calls = %d, want 1 + 3 retries", api.calls.Load())
	}
	want := []time.Duration{15 * time.Minute, 15 * time.Minute, 15 * time.Minute, 12*time.Hour - 45*time.Minute - time.Hour}
	if got := waits.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("final failure not logged as error: %s", logs.String())
	}
	if strings.Contains(logs.String(), testKey) {
		t.Errorf("logs leak the key: %s", logs.String())
	}
}

func TestRunDoesNotRetryIntoNextBoundary(t *testing.T) {
	api := newFakeAPI(t)
	api.failures.Store(1)
	store := &fakeStore{}
	clock := &fakeClock{now: time.Date(2026, 9, 16, 11, 50, 0, 0, time.UTC)}
	p := newTestProvider(store, api.client(), clock, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := installTimer(p, clock, cancel, 2)

	_ = p.Run(ctx, 12*time.Hour)

	want := []time.Duration{10 * time.Minute, 12 * time.Hour}
	if got := waits.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	if api.calls.Load() != 2 || store.count() != 1 {
		t.Errorf("calls = %d, stored = %d; want 2 and 1", api.calls.Load(), store.count())
	}
}

func TestRunWithoutKeyDoesNotRetry(t *testing.T) {
	api := newFakeAPI(t)
	clock := &fakeClock{now: time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)}
	p := newTestProvider(&fakeStore{}, NewClient(api.server.URL, "6", "", nil), clock, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := installTimer(p, clock, cancel, 1)

	_ = p.Run(ctx, 12*time.Hour)

	if got := waits.list(); len(got) != 1 || got[0] != 11*time.Hour {
		t.Errorf("waits = %v, want [11h]", got)
	}
	if api.calls.Load() != 0 {
		t.Errorf("calls = %d, want 0", api.calls.Load())
	}
}

func TestRunLoadsStoredSnapshotWhenAPIIsDown(t *testing.T) {
	api := newFakeAPI(t)
	api.failures.Store(100)
	store := &fakeStore{}
	stored := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	store.add(110, stored)
	clock := &fakeClock{now: time.Date(2026, 9, 16, 11, 59, 0, 0, time.UTC)}
	p := newTestProvider(store, api.client(), clock, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	installTimer(p, clock, cancel, 1)

	_ = p.Run(ctx, 12*time.Hour)

	if got, _ := p.Latest(context.Background()); got == nil || got.ID != 110 {
		t.Errorf("latest = %+v, want id 110", got)
	}
	if gaugeValue(t) != float64(stored.Unix()) {
		t.Errorf("gauge = %v, want %v", gaugeValue(t), float64(stored.Unix()))
	}
}

func TestRunStopsOnCanceledContext(t *testing.T) {
	api := newFakeAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := NewProvider(&fakeStore{}, api.client(), &fakeClock{now: time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)}, nil)

	done := make(chan error, 1)
	go func() { done <- p.Run(ctx, 12*time.Hour) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
