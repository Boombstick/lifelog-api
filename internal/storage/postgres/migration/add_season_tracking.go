package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddSeasonTrackingMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_season_tracking",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&film.SeasonMark{}); err != nil {
				return err
			}
			statements := []string{
				`ALTER TABLE film_entries ADD COLUMN IF NOT EXISTS series_state varchar(16)`,

				`ALTER TABLE film_entries DROP CONSTRAINT IF EXISTS chk_film_entry_series_state`,
				`ALTER TABLE film_entries ADD CONSTRAINT chk_film_entry_series_state
					CHECK (series_state IS NULL OR series_state IN ('watching', 'dropped'))`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Migrator().DropTable("season_marks"); err != nil {
				return err
			}
			if err := db.Exec(`ALTER TABLE film_entries DROP CONSTRAINT IF EXISTS chk_film_entry_series_state`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE film_entries DROP COLUMN IF EXISTS series_state`).Error
		},
	}
}
