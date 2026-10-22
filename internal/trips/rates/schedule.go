package rates

import (
	"context"
	"errors"
	"time"
)

const defaultEvery = 12 * time.Hour

func (p *Provider) Run(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		every = defaultEvery
	}

	if _, err := p.Latest(ctx); err != nil && ctx.Err() == nil {
		p.logger.Warn("exchange rates: cannot load latest snapshot", "error", err)
	}

	p.refreshWithRetries(ctx, every)
	for {
		next := nextBoundary(p.clock.Now(), every)
		if !p.sleep(ctx, next.Sub(p.clock.Now())) {
			return nil
		}
		p.refreshWithRetries(ctx, every)
	}
}

func (p *Provider) refreshWithRetries(ctx context.Context, every time.Duration) {
	boundary := nextBoundary(p.clock.Now(), every)
	for attempt := 1; ; attempt++ {
		err := p.Refresh(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		refreshFailures.Inc()

		retry := attempt <= p.maxRetries &&
			!errors.Is(err, errNotConfigured) &&
			p.clock.Now().Add(p.retryDelay).Before(boundary)
		if !retry {
			p.logger.Error("exchange rates refresh failed", "attempt", attempt, "next_run", boundary, "error", err)
			return
		}
		p.logger.Warn("exchange rates refresh failed, retrying", "attempt", attempt, "retry_in", p.retryDelay.String(), "error", err)
		if !p.sleep(ctx, p.retryDelay) {
			return
		}
	}
}

func (p *Provider) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-p.after(d):
		return ctx.Err() == nil
	}
}

func nextBoundary(now time.Time, every time.Duration) time.Time {
	return now.UTC().Truncate(every).Add(every)
}
