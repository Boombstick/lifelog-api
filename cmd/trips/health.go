package main

import (
	"context"
	"fmt"
	"time"

	"lifelog-api/internal/health"
	"lifelog-api/internal/trips"
	"lifelog-api/internal/trips/category"

	"gorm.io/gorm"
)

const staleRatesAfter = 13 * time.Hour

type latestSnapshot interface {
	LatestSnapshot(ctx context.Context) (*trips.Snapshot, error)
}

func healthChecks(database *gorm.DB, categories *category.Index, snapshots latestSnapshot, jwksURL, photoPath string, ratesKeySet bool) (*health.Registry, error) {
	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}
	return health.Service(sqlDB, jwksURL,

		health.Elasticsearch(false, categories.IndexHealth, categories.Name()),
		health.Check{Name: "exchange-rates", Run: ratesCheck(snapshots, ratesKeySet)},
		health.WritableDir("photo-storage", photoPath),
	), nil
}

func ratesCheck(snapshots latestSnapshot, keySet bool) func(ctx context.Context) health.Result {
	return func(ctx context.Context) health.Result {
		snapshot, err := snapshots.LatestSnapshot(ctx)
		if err != nil {
			return health.Fail("cannot read exchange rate snapshots: " + health.ErrorText(err))
		}
		suffix := ""
		if !keySet {
			suffix = "; TRIPS_RATES_API_KEY is not configured"
		}
		if snapshot == nil {
			return health.Warn("no exchange rate snapshot stored yet" + suffix)
		}
		age := time.Since(snapshot.CreatedAt).Round(time.Minute)
		description := fmt.Sprintf("latest snapshot is %s old", age)
		if age > staleRatesAfter {
			return health.Warn(description + ", refresh is overdue" + suffix)
		}
		return health.Ok(description + suffix)
	}
}
