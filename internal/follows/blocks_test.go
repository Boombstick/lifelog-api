package follows

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeBlockSource struct {
	mu      sync.Mutex
	blocked map[uuid.UUID][]uuid.UUID
	err     error
}

func (s *fakeBlockSource) Blocked(_ context.Context, user uuid.UUID) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.blocked[user], nil
}

func (s *fakeBlockSource) set(user uuid.UUID, ids []uuid.UUID, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocked[user] = ids
	s.err = err
}

func newTestBlockCache(source BlockSource) (*BlockCache, *clock) {
	cache := NewBlockCache(source, Options{TTL: time.Minute, StaleFor: 10 * time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	clk := &clock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	cache.now = clk.now
	return cache, clk
}

func TestBlockCacheUnknownWhenAuthDown(t *testing.T) {
	user := uuid.New()
	source := &fakeBlockSource{blocked: map[uuid.UUID][]uuid.UUID{}, err: errors.New("down")}
	cache, _ := newTestBlockCache(source)

	if set, known := cache.Blocked(context.Background(), user); known || set != nil {
		t.Fatalf("got %v, %v; want unknown", set, known)
	}
}

func TestBlockCacheStaleThenUnknown(t *testing.T) {
	user, other := uuid.New(), uuid.New()
	source := &fakeBlockSource{blocked: map[uuid.UUID][]uuid.UUID{user: {other}}}
	cache, clk := newTestBlockCache(source)
	ctx := context.Background()

	if set, known := cache.Blocked(ctx, user); !known || !has(set, other) {
		t.Fatal("first lookup must load from auth")
	}
	source.set(user, nil, errors.New("down"))
	clk.t = clk.t.Add(5 * time.Minute)
	if set, known := cache.Blocked(ctx, user); !known || !has(set, other) {
		t.Fatal("within StaleFor the last known set is served")
	}
	clk.t = clk.t.Add(6 * time.Minute)
	if _, known := cache.Blocked(ctx, user); known {
		t.Fatal("past StaleFor the answer is unknown")
	}
}

func TestBlockCacheInvalidateSeesNewBlock(t *testing.T) {
	user, other := uuid.New(), uuid.New()
	source := &fakeBlockSource{blocked: map[uuid.UUID][]uuid.UUID{}}
	cache, _ := newTestBlockCache(source)
	ctx := context.Background()

	cache.Blocked(ctx, user)
	source.set(user, []uuid.UUID{other}, nil)
	Invalidators{cache}.Invalidate(user)
	if set, known := cache.Blocked(ctx, user); !known || !has(set, other) {
		t.Fatal("after invalidation the block must be visible")
	}
}
