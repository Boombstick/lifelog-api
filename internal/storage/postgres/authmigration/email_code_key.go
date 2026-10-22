package authmigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func widenEmailCodeKeyMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_email_code_key_300",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`ALTER TABLE auth_email_codes ALTER COLUMN key TYPE varchar(300)`).Error
		},

		Rollback: func(db *gorm.DB) error {
			return db.Exec(`ALTER TABLE auth_email_codes ALTER COLUMN key TYPE varchar(128)`).Error
		},
	}
}
