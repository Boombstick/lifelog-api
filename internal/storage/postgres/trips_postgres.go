package postgres

import (
	"lifelog-api/internal/storage/postgres/tripsmigration"

	"gorm.io/gorm"
)

func NewTripsPostgres(cfg *PgConfig) (*gorm.DB, error) {
	return open(cfg, servicePoolSize, "trips", tripsmigration.Migrate)
}
