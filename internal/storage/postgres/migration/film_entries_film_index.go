package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddFilmEntriesFilmIndexMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_film_entries_film_index",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`CREATE INDEX IF NOT EXISTS idx_film_entries_film ON film_entries (film_id)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP INDEX IF EXISTS idx_film_entries_film`).Error
		},
	}
}
