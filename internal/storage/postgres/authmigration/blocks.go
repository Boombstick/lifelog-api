package authmigration

import (
	"lifelog-api/internal/authsvc"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func blocksMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_blocks",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&authsvc.Block{}); err != nil {
				return err
			}

			if err := db.Exec(`ALTER TABLE auth_blocks
				DROP CONSTRAINT IF EXISTS chk_auth_block_not_self`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE auth_blocks
				ADD CONSTRAINT chk_auth_block_not_self CHECK (blocker_id <> blocked_id)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable("auth_blocks")
		},
	}
}
