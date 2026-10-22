package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func ReworkSeasonTrackingMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "rework_season_tracking",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&film.SeasonEntry{}); err != nil {
				return err
			}
			statements := []string{
				`ALTER TABLE season_entries DROP CONSTRAINT IF EXISTS chk_season_entry_state`,
				`ALTER TABLE season_entries ADD CONSTRAINT chk_season_entry_state
					CHECK (state IN ('watching', 'watched'))`,
				`ALTER TABLE season_entries DROP CONSTRAINT IF EXISTS chk_season_entry_rate`,
				`ALTER TABLE season_entries ADD CONSTRAINT chk_season_entry_rate
					CHECK (rate IS NULL OR (rate BETWEEN 1 AND 10 AND state = 'watched'))`,
				`INSERT INTO season_entries (user_id, season_id, state, watched_at, updated_at)
					SELECT user_id, season_id, 'watched', watched_at, watched_at FROM season_marks
					ON CONFLICT DO NOTHING`,
				`DROP TABLE IF EXISTS season_marks`,
				`UPDATE film_entries SET series_state = NULL WHERE series_state = 'watching'`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&film.SeasonMark{}); err != nil {
				return err
			}
			if err := db.Exec(`INSERT INTO season_marks (user_id, season_id, watched_at)
				SELECT user_id, season_id, COALESCE(watched_at, updated_at) FROM season_entries
				WHERE state = 'watched' ON CONFLICT DO NOTHING`).Error; err != nil {
				return err
			}
			return db.Migrator().DropTable("season_entries")
		},
	}
}
