package migration

import (
	"fmt"

	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddSeriesMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_series",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&film.SeriesDetails{}, &film.Season{}); err != nil {
				return err
			}

			if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_films_kind_tmdb_id ON films (kind, tmdb_id)`).Error; err != nil {
				return err
			}
			return db.Exec(`DROP INDEX IF EXISTS idx_films_tmdb_id`).Error
		},
		Rollback: func(db *gorm.DB) error {
			var series int64
			if err := db.Raw(`SELECT count(*) FROM films WHERE kind = ?`, film.KindSeries).Scan(&series).Error; err != nil {
				return err
			}
			if series > 0 {
				return fmt.Errorf("films: %d series rows, delete them before rolling back add_series", series)
			}
			if err := db.Migrator().DropTable("seasons", "series_details"); err != nil {
				return err
			}
			if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_films_tmdb_id ON films (tmdb_id)`).Error; err != nil {
				return err
			}
			return db.Exec(`DROP INDEX IF EXISTS idx_films_kind_tmdb_id`).Error
		},
	}
}
