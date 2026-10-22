package follows

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var lookups = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "follows_cache_lookups_total",
		Help: "Lookups of a viewer's follow set in the catalog cache, by result",
	},
	[]string{"result"},
)

type Source interface {
	Following(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	FollowersAmong(ctx context.Context, target uuid.UUID, candidates []uuid.UUID) ([]uuid.UUID, error)
}

type Cache struct {
	*setCache
	source Source
}

func NewCache(source Source, opts Options, logger *slog.Logger) *Cache {
	return &Cache{
		setCache: newSetCache("follows", source.Following, lookups, opts, logger),
		source:   source,
	}
}

var empty = map[uuid.UUID]struct{}{}

func (c *Cache) Following(ctx context.Context, viewer uuid.UUID) map[uuid.UUID]struct{} {
	set, known := c.get(ctx, viewer)
	if !known {
		return empty
	}
	return set
}

func (c *Cache) FollowersAmong(ctx context.Context, target uuid.UUID, candidates []uuid.UUID) (map[uuid.UUID]bool, error) {
	result := map[uuid.UUID]bool{}
	if len(candidates) == 0 {
		return result, nil
	}
	loadCtx, cancel := context.WithTimeout(ctx, c.opts.LoadTimeout)
	defer cancel()
	ids, err := c.source.FollowersAmong(loadCtx, target, candidates)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		result[id] = true
	}
	return result, nil
}
