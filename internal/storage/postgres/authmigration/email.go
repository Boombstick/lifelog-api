package authmigration

import (
	"lifelog-api/internal/authsvc"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func emailMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_email",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&authsvc.EmailCode{}, &authsvc.User{}); err != nil {
				return err
			}

			return db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_email
				ON auth_users (lower(email)) WHERE email IS NOT NULL AND merged_into IS NULL`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Migrator().DropTable("auth_email_codes"); err != nil {
				return err
			}
			if err := db.Exec(`DROP INDEX IF EXISTS idx_auth_users_email`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE auth_users DROP COLUMN IF EXISTS email`).Error
		},
	}
}
