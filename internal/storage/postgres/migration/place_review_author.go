package migration

import (
	"fmt"

	"lifelog-api/internal/place"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddPlaceReviewAuthorMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_place_review_author_and_scale",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&place.PlaceReview{}); err != nil {
				return err
			}

			var outOfScale int64
			if err := db.Raw(`SELECT count(*) FROM place_reviews WHERE rate > 5`).Scan(&outOfScale).Error; err != nil {
				return err
			}
			if outOfScale > 0 {
				return fmt.Errorf("place_reviews: %d rows rated above 5, fix them before narrowing the scale", outOfScale)
			}
			if err := db.Exec(`ALTER TABLE place_reviews DROP CONSTRAINT IF EXISTS chk_place_review_rate`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE place_reviews
				ADD CONSTRAINT chk_place_review_rate CHECK (rate BETWEEN 1 AND 5)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE place_reviews DROP CONSTRAINT IF EXISTS chk_place_review_rate`).Error; err != nil {
				return err
			}
			if err := db.Exec(`ALTER TABLE place_reviews
				ADD CONSTRAINT chk_place_review_rate CHECK (rate BETWEEN 1 AND 10)`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE place_reviews DROP COLUMN IF EXISTS user_id`).Error
		},
	}
}
