package memcache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

func newTestTTL(ttl time.Duration, max int) (*TTL[string, int], *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}
	cache := NewTTL[string, int](ttl, max)
	cache.now = clock.Now
	return cache, clock
}

func TestTTLHitAndExpiry(t *testing.T) {
	cache, clock := newTestTTL(time.Minute, 10)
	if _, ok := cache.Get("a"); ok {
		t.Fatal("empty cache returned a value")
	}
	cache.Put("a", 1, cache.Generation())
	if v, ok := cache.Get("a"); !ok || v != 1 {
		t.Fatalf("got %d %v, want 1 true", v, ok)
	}
	clock.t = clock.t.Add(59 * time.Second)
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expired too early")
	}
	clock.t = clock.t.Add(time.Second)
	if _, ok := cache.Get("a"); ok {
		t.Fatal("entry outlived its TTL")
	}
	if cache.Len() != 0 {
		t.Fatal("expired entry was not dropped on read")
	}
}

func TestTTLDeleteRefusesStalePut(t *testing.T) {
	cache, _ := newTestTTL(time.Minute, 10)
	cache.Put("a", 1, cache.Generation())

	gen := cache.Generation()
	cache.Delete("a")
	if _, ok := cache.Get("a"); ok {
		t.Fatal("deleted key is still served")
	}
	if cache.Put("a", 1, gen) {
		t.Fatal("a value read before the delete was put back")
	}
	if _, ok := cache.Get("a"); ok {
		t.Fatal("stale value is served")
	}
	if !cache.Put("a", 2, cache.Generation()) {
		t.Fatal("a fresh put was refused")
	}
}

func TestTTLBound(t *testing.T) {
	cache, clock := newTestTTL(time.Minute, 3)
	for i, key := range []string{"a", "b", "c"} {
		cache.Put(key, i, cache.Generation())
		clock.t = clock.t.Add(time.Second)
	}
	cache.Put("d", 3, cache.Generation())
	if cache.Len() != 3 {
		t.Fatalf("len = %d, want 3", cache.Len())
	}
	if _, ok := cache.Get("a"); ok {
		t.Fatal("the soonest-to-expire entry should have been evicted")
	}
	if _, ok := cache.Get("d"); !ok {
		t.Fatal("the new entry is missing")
	}

	cache.Put("d", 4, cache.Generation())
	if cache.Len() != 3 {
		t.Fatalf("len after overwrite = %d, want 3", cache.Len())
	}
}

func TestLoaderCachesAndCollapsesMisses(t *testing.T) {
	loader := NewLoader[int](time.Minute, 10)
	var calls atomic.Int32
	release := make(chan struct{})
	load := func(context.Context) (int, error) {
		calls.Add(1)
		<-release
		return 42, nil
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if v, err := loader.Get(context.Background(), "k", load); err != nil || v != 42 {
				t.Errorf("got %d %v", v, err)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if v, err := loader.Get(context.Background(), "k", load); err != nil || v != 42 {
		t.Fatalf("got %d %v", v, err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("load ran %d times, want 1", n)
	}
}

func TestLoaderDoesNotCacheErrors(t *testing.T) {
	loader := NewLoader[int](time.Minute, 10)
	fail := errors.New("boom")
	calls := 0
	load := func(context.Context) (int, error) {
		calls++
		if calls == 1 {
			return 0, fail
		}
		return 7, nil
	}
	if _, err := loader.Get(context.Background(), "k", load); !errors.Is(err, fail) {
		t.Fatalf("err = %v, want boom", err)
	}
	if v, err := loader.Get(context.Background(), "k", load); err != nil || v != 7 {
		t.Fatalf("got %d %v, want 7 after the failure", v, err)
	}
}

func TestLoaderSurvivesCallerCancel(t *testing.T) {
	loader := NewLoader[int](time.Minute, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loaded := make(chan struct{})
	_, err := loader.Get(ctx, "k", func(ctx context.Context) (int, error) {
		defer close(loaded)
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 1, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a caller that left got %v, want context.Canceled", err)
	}
	<-loaded

	deadline := time.Now().Add(time.Second)
	for {
		if v, ok := loader.Cached("k"); ok {
			if v != 1 {
				t.Fatalf("cached %d, want 1", v)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a cancelled caller failed the shared load: nothing cached")
		}
		time.Sleep(time.Millisecond)
	}
}
