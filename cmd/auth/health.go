package main

import (
	"context"
	"time"

	"lifelog-api/internal/authsvc"
	"lifelog-api/internal/config"
	"lifelog-api/internal/health"
	"lifelog-api/internal/token"

	"gorm.io/gorm"
)

func healthChecks(database *gorm.DB, signer *token.Signer, configuration *config.Config) (*health.Registry, error) {
	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}

	var smtp *authsvc.SMTPMailer
	if configuration.AuthSMTPHost != "" {
		smtp = authsvc.NewSMTPMailer(configuration.AuthSMTPHost, configuration.AuthSMTPPort, "", "", "")
	}

	return health.New(
		health.Postgres(sqlDB),
		health.Check{Name: "signing-key", Critical: true, Run: func(context.Context) health.Result {
			if signer.KeyID() == "" {
				return health.Fail("signing key is not loaded")
			}
			return health.Ok("signing key loaded, kid " + signer.KeyID())
		}},
		health.Check{
			Name: "smtp",

			Timeout: 5 * time.Second,
			Run: health.Cached(health.ThirdPartyTTL, func(ctx context.Context) health.Result {
				if smtp == nil {
					return health.Ok("not configured, codes are logged")
				}
				if err := smtp.Probe(ctx); err != nil {
					return health.Fail("SMTP is unavailable: " + health.ErrorText(err, configuration.AuthSMTPPassword))
				}
				return health.Ok("SMTP answers and offers TLS")
			}),
		},
	), nil
}
