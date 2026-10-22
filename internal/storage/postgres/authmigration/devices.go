package authmigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func devicesMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_session_devices",
		Migrate: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE auth_sessions ADD COLUMN IF NOT EXISTS device_id varchar(64)`).Error; err != nil {
				return err
			}
			return db.Exec(`CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_device
				ON auth_sessions (user_id, device_id) WHERE device_id IS NOT NULL AND revoked_at IS NULL`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`DROP INDEX IF EXISTS idx_auth_sessions_user_device`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE auth_sessions DROP COLUMN IF EXISTS device_id`).Error
		},
	}
}
