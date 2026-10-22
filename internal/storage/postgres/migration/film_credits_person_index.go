package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddFilmCreditsPersonIndexMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_film_credits_person_index",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`CREATE INDEX IF NOT EXISTS idx_film_credits_person ON film_credits (person_id)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP INDEX IF EXISTS idx_film_credits_person`).Error
		},
	}
}
