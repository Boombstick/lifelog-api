package main

import (
	"context"
	"time"

	"lifelog-api/internal/config"
	"lifelog-api/internal/health"
	"lifelog-api/internal/notify"

	"gorm.io/gorm"
)

func healthChecks(database *gorm.DB, fcm *notify.FCMSender, webPush bool, configuration *config.Config) (*health.Registry, error) {
	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}
	return health.Service(sqlDB, configuration.AuthJWKSURL,
		health.Check{Name: "fcm", Timeout: 12 * time.Second, Run: health.Cached(health.ThirdPartyTTL, func(ctx context.Context) health.Result {
			if fcm == nil {
				return health.Ok("not configured, pushes are logged")
			}
			if err := fcm.Ping(ctx); err != nil {
				return health.Fail("FCM access token unavailable: " + health.ErrorText(err))
			}
			return health.Ok("access token issued for project " + fcm.ProjectID())
		})},

		health.Check{Name: "webpush", Timeout: time.Second, Run: func(context.Context) health.Result {
			if !webPush {
				return health.Ok("not configured, web push is off")
			}
			return health.Ok("VAPID keys loaded")
		}},
	), nil
}
