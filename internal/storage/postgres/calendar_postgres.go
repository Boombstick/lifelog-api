package postgres

import (
	"lifelog-api/internal/storage/postgres/calendarmigration"

	"gorm.io/gorm"
)

func NewCalendarPostgres(cfg *PgConfig) (*gorm.DB, error) {
	return open(cfg, servicePoolSize, "calendar", calendarmigration.Migrate)
}
