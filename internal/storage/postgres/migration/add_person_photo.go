package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddPersonPhotoMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_person_photo",
		Migrate: func(db *gorm.DB) error {
			return db.AutoMigrate(&film.Person{})
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropColumn(&film.Person{}, "photo_url")
		},
	}
}
