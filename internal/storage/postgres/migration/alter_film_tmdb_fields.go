package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AlterFilmTMDBFieldsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "alter_film_tmdb_fields",
		Migrate: func(db *gorm.DB) error {
			return db.AutoMigrate(&film.Film{})
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Migrator().DropIndex(&film.Film{}, "TMDBPopularity"); err != nil {
				return err
			}
			if err := db.Migrator().DropIndex(&film.Film{}, "IMDBID"); err != nil {
				return err
			}
			return db.Exec(
				"ALTER TABLE films ALTER COLUMN tmdb_popularity TYPE bigint USING tmdb_popularity::bigint",
			).Error
		},
	}
}
