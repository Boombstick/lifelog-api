package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddShareLinkExpiryMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_share_link_expiry",
		Migrate: func(db *gorm.DB) error {
			steps := []string{
				`ALTER TABLE share_links ADD COLUMN IF NOT EXISTS expires_at timestamptz`,
				`UPDATE share_links SET expires_at = created_at + interval '24 hours' WHERE expires_at IS NULL`,
				`ALTER TABLE share_links ALTER COLUMN expires_at SET NOT NULL`,
				`CREATE INDEX IF NOT EXISTS idx_share_links_expires_at ON share_links (expires_at)`,
			}
			for _, step := range steps {
				if err := db.Exec(step).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`ALTER TABLE share_links DROP COLUMN IF EXISTS expires_at`).Error
		},
	}
}
