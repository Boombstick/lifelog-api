package migration

import (
	"lifelog-api/internal/place"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AppendReviewToPlace() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "append_review_to_place",
		Migrate: func(db *gorm.DB) error {
			err := db.AutoMigrate(&place.PlaceReview{})
			if err != nil {
				return err
			}

			if !db.Migrator().HasIndex(&place.PlaceReview{}, "PlaceID") {
				if err := db.Migrator().CreateIndex(&place.PlaceReview{}, "PlaceID"); err != nil {
					return err
				}
			}
			if db.Migrator().HasConstraint(&place.Place{}, "Reviews") {
				return nil
			}
			return db.Migrator().CreateConstraint(&place.Place{}, "Reviews")
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable("place_reviews")
		},
	}
}
