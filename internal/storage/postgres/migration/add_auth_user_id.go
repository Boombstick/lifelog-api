package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddAuthUserIDMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_auth_user_id",
		Migrate: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_user_id uuid`).Error; err != nil {
				return err
			}

			return db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_auth_user_id
				ON users (auth_user_id) WHERE auth_user_id IS NOT NULL`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`DROP INDEX IF EXISTS idx_users_auth_user_id`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS auth_user_id`).Error
		},
	}
}
