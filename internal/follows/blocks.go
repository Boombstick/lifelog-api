package follows

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var blockLookups = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "blocks_cache_lookups_total",
		Help: "Lookups of a user's block relations in the catalog cache, by result",
	},
	[]string{"result"},
)

type BlockSource interface {
	Blocked(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

type BlockCache struct {
	*setCache
}

func NewBlockCache(source BlockSource, opts Options, logger *slog.Logger) *BlockCache {
	return &BlockCache{setCache: newSetCache("blocks", source.Blocked, blockLookups, opts, logger)}
}

func (c *BlockCache) Blocked(ctx context.Context, user uuid.UUID) (map[uuid.UUID]struct{}, bool) {
	return c.get(ctx, user)
}
