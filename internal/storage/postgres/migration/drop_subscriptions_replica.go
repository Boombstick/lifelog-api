package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func DropSubscriptionsReplicaMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "drop_subscriptions_replica",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`DROP TABLE IF EXISTS subscriptions`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`CREATE TABLE IF NOT EXISTS subscriptions (
				follower_auth_id uuid NOT NULL,
				target_auth_id uuid NOT NULL,
				updated_at timestamptz NOT NULL,
				PRIMARY KEY (follower_auth_id, target_auth_id),
				CONSTRAINT chk_subscription_not_self CHECK (follower_auth_id <> target_auth_id))`).Error; err != nil {
				return err
			}
			return db.Exec(`CREATE INDEX IF NOT EXISTS idx_subscriptions_target_auth_id
				ON subscriptions (target_auth_id)`).Error
		},
	}
}
