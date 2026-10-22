package main

import (
	"lifelog-api/internal/config"
	"lifelog-api/internal/health"

	"gorm.io/gorm"
)

func healthChecks(database *gorm.DB, configuration *config.Config) (*health.Registry, error) {
	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}
	return health.Service(sqlDB, configuration.AuthJWKSURL), nil
}
