package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func DropUserCredentialsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "drop_user_credentials",
		Migrate: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS password_hash`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS email`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS email varchar(255) NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash text`).Error
		},
	}
}
