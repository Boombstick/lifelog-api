package migration

import (
	"lifelog-api/internal/auth"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddUserMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_user",
		Migrate: func(db *gorm.DB) error {
			err := db.AutoMigrate(&auth.User{})
			if err != nil {
				return err
			}

			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable("users")
		},
	}
}
