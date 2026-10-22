package place

import "gorm.io/gorm"

type PlaceReview struct {
	gorm.Model
	Description string `gorm:"size:255;not null"`
	Rate        int16  `gorm:"not null;check:chk_place_review_rate,rate BETWEEN 1 AND 5"`
	PlaceID     uint   `gorm:"not null;index"`

	UserID *uint `gorm:"index"`
}
