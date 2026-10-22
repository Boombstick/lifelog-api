package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddFilmRatingsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_film_ratings",
		Migrate: func(db *gorm.DB) error {
			return db.AutoMigrate(&film.Film{})
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropColumn(&film.Film{}, "runtime")
		},
	}
}
