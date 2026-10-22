package postgres

import (
	"lifelog-api/internal/storage/postgres/notesmigration"

	"gorm.io/gorm"
)

func NewNotesPostgres(cfg *PgConfig) (*gorm.DB, error) {
	return open(cfg, servicePoolSize, "notes", notesmigration.Migrate)
}
