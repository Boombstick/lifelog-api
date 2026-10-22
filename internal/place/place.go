package place

import (
	"errors"
	"slices"

	"lifelog-api/internal/geo"
	"lifelog-api/internal/model"

	"gorm.io/gorm"
)

type Kind string

const (
	KindFood    Kind = "food"
	KindCulture Kind = "culture"
	KindPark    Kind = "park"
	KindStay    Kind = "stay"
	KindOther   Kind = "other"
)

var KnownKinds = []Kind{KindFood, KindCulture, KindPark, KindStay, KindOther}

func (k Kind) Valid() bool {
	return slices.Contains(KnownKinds, k)
}

type Place struct {
	gorm.Model
	Name    string `gorm:"size:128"`
	Address string `gorm:"size:128"`

	CountryCode      *string      `gorm:"size:2;index"`
	CityID           *uint        `gorm:"index"`
	Country          *geo.Country `gorm:"foreignKey:CountryCode;references:Code"`
	City             *geo.City    `gorm:"foreignKey:CityID"`
	Latitude         *float64
	Longitude        *float64
	ModerationStatus model.ModerationStatus `gorm:"size:16;not null;default:pending;index"`
	Kind             Kind                   `gorm:"size:16;not null;default:other;index"`
	Category         string                 `gorm:"size:32;not null"`
	Reviews          []PlaceReview          `gorm:"foreignKey:PlaceID"`

	CreatedBy *uint `gorm:"index"`
}

type Filter struct {
	CountryCode string
	CityID      uint
	Kind        string
	Query       string

	AfterID uint
	Limit   int
	Viewer  Viewer
}

type Viewer struct {
	UserID uint
	Admin  bool
}

func (v Viewer) CanEdit(p Place) bool {
	if v.Admin {
		return true
	}
	return v.UserID != 0 && p.CreatedBy != nil && *p.CreatedBy == v.UserID
}

var ErrUnknownGeography = errors.New("такой страны или города нет в справочнике")
