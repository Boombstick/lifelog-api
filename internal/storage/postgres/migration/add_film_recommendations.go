package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddFilmRecommendationsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_film_recommendations",
		Migrate: func(db *gorm.DB) error {
			return db.AutoMigrate(&film.FilmRecommendation{})
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable("film_recommendations")
		},
	}
}
