package follows

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeSource struct {
	mu        sync.Mutex
	following map[uuid.UUID][]uuid.UUID
	err       error
	calls     atomic.Int32

	gate chan struct{}
}

func (s *fakeSource) Following(_ context.Context, user uuid.UUID) ([]uuid.UUID, error) {
	s.calls.Add(1)
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.following[user], nil
}

func (s *fakeSource) FollowersAmong(context.Context, uuid.UUID, []uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (s *fakeSource) set(user uuid.UUID, ids []uuid.UUID, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.following[user] = ids
	s.err = err
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestCache(source Source) (*Cache, *clock) {
	cache := NewCache(source, Options{TTL: time.Minute, StaleFor: 10 * time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	clk := &clock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	cache.now = clk.now
	return cache, clk
}

func has(set map[uuid.UUID]struct{}, id uuid.UUID) bool {
	_, ok := set[id]
	return ok
}

func TestCacheServesWithinTTLAndReloadsAfter(t *testing.T) {
	viewer, target := uuid.New(), uuid.New()
	source := &fakeSource{following: map[uuid.UUID][]uuid.UUID{viewer: {target}}}
	cache, clk := newTestCache(source)
	ctx := context.Background()

	if !has(cache.Following(ctx, viewer), target) {
		t.Fatal("first lookup must load from auth")
	}
	source.set(viewer, nil, nil)
	clk.t = clk.t.Add(59 * time.Second)
	if !has(cache.Following(ctx, viewer), target) {
		t.Fatal("within TTL the cached answer is used")
	}
	clk.t = clk.t.Add(time.Second)
	if has(cache.Following(ctx, viewer), target) {
		t.Fatal("after TTL the change in auth must be seen without any invalidation")
	}
	if got := source.calls.Load(); got != 2 {
		t.Fatalf("auth calls = %d, want 2", got)
	}
}

func TestCacheServesLastKnownWhileAuthIsDownThenFailsClosed(t *testing.T) {
	viewer, target := uuid.New(), uuid.New()
	source := &fakeSource{following: map[uuid.UUID][]uuid.UUID{viewer: {target}}}
	cache, clk := newTestCache(source)
	ctx := context.Background()
	cache.Following(ctx, viewer)

	source.set(viewer, []uuid.UUID{target}, errors.New("auth down"))
	clk.t = clk.t.Add(9 * time.Minute)
	if !has(cache.Following(ctx, viewer), target) {
		t.Fatal("a last-known answer younger than StaleFor must be served")
	}
	clk.t = clk.t.Add(time.Minute)
	if len(cache.Following(ctx, viewer)) != 0 {
		t.Fatal("past StaleFor the viewer must follow nobody")
	}
}

func TestInvalidateDropsTheFallbackToo(t *testing.T) {
	viewer, target := uuid.New(), uuid.New()
	source := &fakeSource{following: map[uuid.UUID][]uuid.UUID{viewer: {target}}}
	cache, _ := newTestCache(source)
	ctx := context.Background()
	cache.Following(ctx, viewer)

	source.set(viewer, nil, errors.New("auth down"))
	cache.Invalidate(viewer)
	if len(cache.Following(ctx, viewer)) != 0 {
		t.Fatal("after an invalidation the old set must not come back as a fallback")
	}
}

func TestLoadStartedBeforeInvalidationIsNotCached(t *testing.T) {
	viewer, target := uuid.New(), uuid.New()
	source := &fakeSource{following: map[uuid.UUID][]uuid.UUID{viewer: {target}}, gate: make(chan struct{})}
	cache, _ := newTestCache(source)
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		cache.Following(ctx, viewer)
		close(done)
	}()
	for source.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cache.Invalidate(viewer)
	close(source.gate)
	<-done

	source.set(viewer, nil, nil)
	if has(cache.Following(ctx, viewer), target) {
		t.Fatal("an answer loaded before the invalidation was put back into the cache")
	}
}

func TestConcurrentMissesShareOneLoad(t *testing.T) {
	viewer := uuid.New()
	source := &fakeSource{following: map[uuid.UUID][]uuid.UUID{viewer: {uuid.New()}}, gate: make(chan struct{})}
	cache, _ := newTestCache(source)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Following(context.Background(), viewer)
		}()
	}
	for source.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(source.gate)
	wg.Wait()
	if got := source.calls.Load(); got != 1 {
		t.Fatalf("auth calls = %d, want 1", got)
	}
}

func TestCacheIsBounded(t *testing.T) {
	source := &fakeSource{following: map[uuid.UUID][]uuid.UUID{}}
	cache := NewCache(source, Options{MaxEntries: 3}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for range 5 {
		cache.Following(context.Background(), uuid.New())
	}
	if len(cache.items) != 3 {
		t.Fatalf("entries = %d, want 3", len(cache.items))
	}
}
