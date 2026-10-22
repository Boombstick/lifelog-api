package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddPlaceAuthorMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_place_author",
		Migrate: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE places ADD COLUMN IF NOT EXISTS created_by bigint`).Error; err != nil {
				return err
			}
			return db.Exec(`CREATE INDEX IF NOT EXISTS idx_places_created_by ON places (created_by)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`DROP INDEX IF EXISTS idx_places_created_by`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE places DROP COLUMN IF EXISTS created_by`).Error
		},
	}
}
