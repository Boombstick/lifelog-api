package migration

import (
	"lifelog-api/internal/geo"
	"lifelog-api/internal/place"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddGeoAndPlaceFieldsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_geo_and_place_fields",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&geo.Country{}, &geo.City{}); err != nil {
				return err
			}
			return db.AutoMigrate(&place.Place{})
		},
		Rollback: func(db *gorm.DB) error {
			for _, column := range []string{"country_code", "city_id", "kind"} {
				if db.Migrator().HasColumn(&place.Place{}, column) {
					if err := db.Migrator().DropColumn(&place.Place{}, column); err != nil {
						return err
					}
				}
			}
			return db.Migrator().DropTable(&geo.City{}, &geo.Country{})
		},
	}
}
