package main

import (
	"context"
	"net/http"
	"time"

	"lifelog-api/internal/calendar"
	"lifelog-api/internal/config"
	"lifelog-api/internal/health"

	"gorm.io/gorm"
)

const googleDiscoveryURL = "https://accounts.google.com/.well-known/openid-configuration"

func healthChecks(database *gorm.DB, addresses *calendar.AuthClient, telegram *calendar.TelegramMessenger, notify *calendar.NotifyClient, configuration *config.Config) (*health.Registry, error) {
	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: 5 * time.Second}

	return health.Service(sqlDB, configuration.AuthJWKSURL,

		health.Check{Name: "auth-internal", Run: func(ctx context.Context) health.Result {
			if err := addresses.Ping(ctx); err != nil {
				return health.Fail("auth internal lookup failed: " + health.ErrorText(err, configuration.AuthInternalToken))
			}
			return health.Ok("auth internal lookup works")
		}},
		health.Check{Name: "notify", Run: func(ctx context.Context) health.Result {
			if notify == nil {
				return health.Ok("not configured, push is off")
			}
			if err := notify.Ping(ctx); err != nil {
				return health.Fail("notify service unavailable: " + health.ErrorText(err))
			}
			return health.Ok("notify service answers")
		}},
		health.Check{Name: "telegram", Run: health.Cached(health.ThirdPartyTTL, func(ctx context.Context) health.Result {
			if telegram == nil {
				return health.Ok("not configured, notifications are logged")
			}
			if err := telegram.Ping(ctx); err != nil {
				return health.Fail("Telegram is unavailable: " + health.ErrorText(err,
					configuration.TelegramBotToken, configuration.TelegramRelaySecret))
			}
			via := "Telegram API"
			if configuration.TelegramAPIBaseURL != "" {
				via = "the relay"
			}
			return health.Ok("getMe answered through " + via)
		})},
		health.Check{Name: "google", Run: health.Cached(health.ThirdPartyTTL, func(ctx context.Context) health.Result {
			if configuration.CalendarGoogleClientID == "" {
				return health.Ok("not configured, import is off")
			}
			if err := health.Reachable(ctx, httpClient, googleDiscoveryURL); err != nil {
				return health.Fail("Google OAuth is unavailable: " + health.ErrorText(err))
			}
			return health.Ok("Google OAuth discovery is available")
		})},
	), nil
}
