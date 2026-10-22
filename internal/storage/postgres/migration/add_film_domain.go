package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddFilmDomainMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_film_domain",
		Migrate: func(db *gorm.DB) error {
			err := db.AutoMigrate(
				&film.Genre{},
				&film.Person{},
				&film.Film{},
				&film.FilmCredit{},
				&film.FilmEntry{},
			)
			if err != nil {
				return err
			}

			if db.Migrator().HasConstraint(&film.FilmEntry{}, "User") {
				return nil
			}
			return db.Migrator().CreateConstraint(&film.FilmEntry{}, "User")
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable(
				"film_entries",
				"film_credits",
				"film_genres",
				"people",
				"genres",
				"films",
			)
		},
	}
}
