package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"lifelog-api/internal/health"
	"lifelog-api/internal/search"
	"lifelog-api/internal/tmdb"

	"gorm.io/gorm"
)

func healthChecks(database *gorm.DB, searchEngine *search.Client, jwksURL string, tmdbClient *tmdb.Client, tmdbToken string) (*health.Registry, error) {
	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}
	return health.Service(sqlDB, jwksURL,
		health.Elasticsearch(true, searchEngine.IndexHealth, searchEngine.FilmsIndex(), searchEngine.PeopleIndex()),
		health.Check{
			Name: "tmdb",
			Run:  health.Cached(health.ThirdPartyTTL, tmdbCheck(tmdbClient, tmdbToken)),
		},
	), nil
}

func tmdbCheck(client *tmdb.Client, token string) func(ctx context.Context) health.Result {
	return func(ctx context.Context) health.Result {
		if client == nil {
			return health.Ok("not configured, ratings stay local")
		}
		err := client.Ping(ctx)
		if err == nil {
			return health.Ok("TMDB is available")
		}
		var apiErr *tmdb.APIError
		if errors.As(err, &apiErr) {
			return health.Fail(fmt.Sprintf("TMDB answered %d", apiErr.StatusCode))
		}
		text := health.ErrorText(err, token)

		if strings.Contains(text, "127.0.0.1") || strings.Contains(text, "[::1]") {
			text += " (TMDB resolves to localhost: DNS poisoning on this host?)"
		}
		return health.Fail("TMDB is unavailable: " + text)
	}
}
