package rates

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"lifelog-api/internal/trips"
)

const baseCurrency = "USD"

type SnapshotStore interface {
	LatestSnapshot(ctx context.Context) (*trips.Snapshot, error)

	SnapshotsAround(ctx context.Context, at time.Time) (before, after *trips.Snapshot, err error)

	InsertSnapshot(ctx context.Context, snapshot *trips.Snapshot) error
}

type Provider struct {
	store  SnapshotStore
	client *Client
	clock  trips.Clock
	logger *slog.Logger

	current atomic.Pointer[trips.Snapshot]

	after      func(time.Duration) <-chan time.Time
	retryDelay time.Duration
	maxRetries int
}

func NewProvider(store SnapshotStore, client *Client, clock trips.Clock, logger *slog.Logger) *Provider {
	if clock == nil {
		clock = trips.SystemClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		store:      store,
		client:     client,
		clock:      clock,
		logger:     logger,
		after:      time.After,
		retryDelay: 15 * time.Minute,
		maxRetries: 3,
	}
}

var _ trips.Rates = (*Provider)(nil)

func (p *Provider) Latest(ctx context.Context) (*trips.Snapshot, error) {
	if snapshot := p.current.Load(); snapshot != nil {
		return snapshot, nil
	}
	loaded, err := p.store.LatestSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("load latest exchange rate snapshot: %w", err)
	}
	if loaded == nil {
		return nil, nil
	}

	if p.current.CompareAndSwap(nil, loaded) {
		setSnapshotGauge(loaded)
		return loaded, nil
	}
	return p.current.Load(), nil
}

func (p *Provider) Closest(ctx context.Context, at time.Time) (*trips.Snapshot, error) {
	at = at.UTC()
	before, after, err := p.store.SnapshotsAround(ctx, at)
	if err != nil {
		return nil, fmt.Errorf("load exchange rate snapshots around %s: %w", at.Format(time.RFC3339), err)
	}
	return pickClosest(at, before, after), nil
}

func pickClosest(at time.Time, before, after *trips.Snapshot) *trips.Snapshot {
	switch {
	case before == nil:
		return after
	case after == nil:
		return before
	case at.Sub(before.CreatedAt) <= after.CreatedAt.Sub(at):
		return before
	default:
		return after
	}
}

func (p *Provider) Refresh(ctx context.Context) error {
	rates, err := p.client.Latest(ctx)
	if err != nil {
		return err
	}
	snapshot := &trips.Snapshot{BaseCurrency: baseCurrency, Rates: rates, CreatedAt: p.clock.Now()}
	if err := p.store.InsertSnapshot(ctx, snapshot); err != nil {
		return fmt.Errorf("store exchange rate snapshot: %w", err)
	}
	p.current.Store(snapshot)
	setSnapshotGauge(snapshot)
	p.logger.Info("exchange rates updated", "snapshot_id", snapshot.ID, "rates", len(rates))
	return nil
}

func setSnapshotGauge(snapshot *trips.Snapshot) {
	snapshotTimestamp.Set(float64(snapshot.CreatedAt.UnixNano()) / float64(time.Second))
}
