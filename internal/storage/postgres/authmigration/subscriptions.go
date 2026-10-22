package authmigration

import (
	"lifelog-api/internal/authsvc"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func subscriptionsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_subscriptions",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&authsvc.Subscription{}, &authsvc.User{}); err != nil {
				return err
			}

			if err := db.Exec(`ALTER TABLE auth_subscriptions
				DROP CONSTRAINT IF EXISTS chk_auth_subscription_not_self`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE auth_subscriptions
				ADD CONSTRAINT chk_auth_subscription_not_self
				CHECK (follower_id <> target_id)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Migrator().DropTable("auth_subscriptions"); err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE auth_users DROP COLUMN IF EXISTS is_private`).Error
		},
	}
}
