package migration

import (
	"lifelog-api/internal/auth"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddShareLinksMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_share_links",
		Migrate: func(db *gorm.DB) error {
			return db.AutoMigrate(&auth.ShareLink{})
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable("share_links")
		},
	}
}
