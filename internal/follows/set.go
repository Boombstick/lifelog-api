package follows

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/singleflight"
)

const (
	resultHit    = "hit"
	resultMiss   = "miss"
	resultStale  = "stale"
	resultClosed = "closed"
)

type Options struct {
	TTL time.Duration

	StaleFor time.Duration

	MaxEntries int

	LoadTimeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.TTL <= 0 {
		o.TTL = time.Minute
	}
	if o.StaleFor < o.TTL {
		o.StaleFor = 10 * time.Minute
	}
	if o.MaxEntries <= 0 {
		o.MaxEntries = 10000
	}
	if o.LoadTimeout <= 0 {
		o.LoadTimeout = 3 * time.Second
	}
	return o
}

type entry struct {
	set     map[uuid.UUID]struct{}
	fetched time.Time
}

type setCache struct {
	name    string
	load    func(ctx context.Context, user uuid.UUID) ([]uuid.UUID, error)
	lookups *prometheus.CounterVec
	opts    Options
	logger  *slog.Logger
	now     func() time.Time

	mu    sync.Mutex
	items map[uuid.UUID]entry

	gen   uint64
	group singleflight.Group
}

func newSetCache(name string, load func(context.Context, uuid.UUID) ([]uuid.UUID, error), lookups *prometheus.CounterVec, opts Options, logger *slog.Logger) *setCache {
	if logger == nil {
		logger = slog.Default()
	}
	return &setCache{
		name:    name,
		load:    load,
		lookups: lookups,
		opts:    opts.withDefaults(),
		logger:  logger,
		now:     time.Now,
		items:   map[uuid.UUID]entry{},
	}
}

func (c *setCache) get(ctx context.Context, user uuid.UUID) (map[uuid.UUID]struct{}, bool) {
	now := c.now()
	c.mu.Lock()
	cached, ok := c.items[user]
	c.mu.Unlock()
	if ok && now.Sub(cached.fetched) < c.opts.TTL {
		c.lookups.WithLabelValues(resultHit).Inc()
		return cached.set, true
	}

	result, err, _ := c.group.Do(user.String(), func() (any, error) {
		c.mu.Lock()
		gen := c.gen
		c.mu.Unlock()

		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.opts.LoadTimeout)
		defer cancel()
		ids, err := c.load(loadCtx, user)
		if err != nil {
			return nil, err
		}
		set := make(map[uuid.UUID]struct{}, len(ids))
		for _, id := range ids {
			set[id] = struct{}{}
		}

		c.mu.Lock()
		if gen == c.gen {
			c.put(user, entry{set: set, fetched: c.now()})
		}
		c.mu.Unlock()
		return set, nil
	})
	if err == nil {
		c.lookups.WithLabelValues(resultMiss).Inc()
		return result.(map[uuid.UUID]struct{}), true
	}

	c.mu.Lock()
	cached, ok = c.items[user]
	c.mu.Unlock()
	if ok && c.now().Sub(cached.fetched) < c.opts.StaleFor {
		c.lookups.WithLabelValues(resultStale).Inc()
		c.logger.Warn(c.name+": auth unavailable, serving last known set", "user", user, "age", c.now().Sub(cached.fetched).String(), "error", err)
		return cached.set, true
	}
	c.lookups.WithLabelValues(resultClosed).Inc()
	c.logger.Error(c.name+": auth unavailable, answer unknown", "user", user, "error", err)
	return nil, false
}

func (c *setCache) Invalidate(users ...uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, user := range users {
		delete(c.items, user)
	}
	c.gen++
}

func (c *setCache) put(user uuid.UUID, e entry) {
	if _, exists := c.items[user]; !exists && len(c.items) >= c.opts.MaxEntries {
		now := c.now()
		for key, old := range c.items {
			if now.Sub(old.fetched) >= c.opts.StaleFor {
				delete(c.items, key)
			}
		}
		if len(c.items) >= c.opts.MaxEntries {
			for key := range c.items {
				delete(c.items, key)
				break
			}
		}
	}
	c.items[user] = e
}

type Invalidators []interface{ Invalidate(users ...uuid.UUID) }

func (all Invalidators) Invalidate(users ...uuid.UUID) {
	for _, cache := range all {
		if cache != nil {
			cache.Invalidate(users...)
		}
	}
}
