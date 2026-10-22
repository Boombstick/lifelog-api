package migration

import (
	"lifelog-api/internal/place"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func InitialMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "initial_migration",
		Migrate: func(db *gorm.DB) error {
			err := db.AutoMigrate(&place.Place{})
			if err != nil {
				return err
			}

			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable("places")
		},
	}
}
