package postgres

import (
	"lifelog-api/internal/storage/postgres/authmigration"

	"gorm.io/gorm"
)

func NewAuthPostgres(cfg *PgConfig) (*gorm.DB, error) {
	return open(cfg, servicePoolSize, "auth", authmigration.Migrate)
}
