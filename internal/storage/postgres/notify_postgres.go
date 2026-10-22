package postgres

import (
	"lifelog-api/internal/storage/postgres/notifymigration"

	"gorm.io/gorm"
)

func NewNotifyPostgres(cfg *PgConfig) (*gorm.DB, error) {
	return open(cfg, servicePoolSize, "notify", notifymigration.Migrate)
}
