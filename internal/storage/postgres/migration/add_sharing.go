package migration

import (
	"lifelog-api/internal/auth"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddSharingMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_sharing",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&auth.Subscription{}, &auth.ListVisibility{}); err != nil {
				return err
			}

			if err := db.Exec(`ALTER TABLE subscriptions
				DROP CONSTRAINT IF EXISTS chk_subscription_not_self`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE subscriptions
				ADD CONSTRAINT chk_subscription_not_self
				CHECK (follower_auth_id <> target_auth_id)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable("subscriptions", "list_visibility")
		},
	}
}
