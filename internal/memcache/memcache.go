package memcache

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const evictionSample = 32

type TTL[K comparable, V any] struct {
	ttl time.Duration
	max int
	now func() time.Time

	mu    sync.Mutex
	items map[K]ttlEntry[V]

	gen uint64
}

type ttlEntry[V any] struct {
	value   V
	expires time.Time
}

func NewTTL[K comparable, V any](ttl time.Duration, maxEntries int) *TTL[K, V] {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &TTL[K, V]{ttl: ttl, max: maxEntries, now: time.Now, items: map[K]ttlEntry[V]{}}
}

func (c *TTL[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	if !c.now().Before(entry.expires) {
		delete(c.items, key)
		var zero V
		return zero, false
	}
	return entry.value, true
}

func (c *TTL[K, V]) Generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *TTL[K, V]) Put(key K, value V, gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return false
	}
	now := c.now()
	if _, exists := c.items[key]; !exists && len(c.items) >= c.max {
		c.evict(now)
	}
	c.items[key] = ttlEntry[V]{value: value, expires: now.Add(c.ttl)}
	return true
}

func (c *TTL[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[K]ttlEntry[V]{}
	c.gen++
}

func (c *TTL[K, V]) Delete(keys ...K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.items, key)
	}
	c.gen++
}

func (c *TTL[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *TTL[K, V]) evict(now time.Time) {
	var (
		victim   K
		earliest time.Time
		seen     int
	)
	for key, entry := range c.items {
		if !now.Before(entry.expires) {
			delete(c.items, key)
			return
		}
		if seen == 0 || entry.expires.Before(earliest) {
			victim, earliest = key, entry.expires
		}
		seen++
		if seen == evictionSample {
			break
		}
	}
	delete(c.items, victim)
}

const loadTimeout = 10 * time.Second

type Loader[V any] struct {
	cache *TTL[string, V]
	group singleflight.Group
}

func NewLoader[V any](ttl time.Duration, maxEntries int) *Loader[V] {
	return &Loader[V]{cache: NewTTL[string, V](ttl, maxEntries)}
}

func (l *Loader[V]) Get(ctx context.Context, key string, load func(ctx context.Context) (V, error)) (V, error) {
	if value, ok := l.cache.Get(key); ok {
		return value, nil
	}
	ch := l.group.DoChan(key, func() (any, error) {
		if value, ok := l.cache.Get(key); ok {
			return value, nil
		}
		gen := l.cache.Generation()
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loadTimeout)
		defer cancel()
		value, err := load(loadCtx)
		if err != nil {
			return nil, err
		}
		l.cache.Put(key, value, gen)
		return value, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			var zero V
			return zero, res.Err
		}
		return res.Val.(V), nil
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

func (l *Loader[V]) Clear() {
	l.cache.Clear()
}

func (l *Loader[V]) Cached(key string) (V, bool) {
	return l.cache.Get(key)
}
