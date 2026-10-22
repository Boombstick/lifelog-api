package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddFilmsManualAddedMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_films_manual_added",
		Migrate: func(db *gorm.DB) error {
			statements := []string{
				`ALTER TABLE films ADD COLUMN IF NOT EXISTS added_by bigint REFERENCES users (id) ON DELETE SET NULL`,
				`ALTER TABLE films ADD COLUMN IF NOT EXISTS added_at timestamptz`,
				`CREATE INDEX IF NOT EXISTS idx_films_added_by_at ON films (added_by, added_at) WHERE added_by IS NOT NULL`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			statements := []string{
				`DROP INDEX IF EXISTS idx_films_added_by_at`,
				`ALTER TABLE films DROP COLUMN IF EXISTS added_at`,
				`ALTER TABLE films DROP COLUMN IF EXISTS added_by`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}
