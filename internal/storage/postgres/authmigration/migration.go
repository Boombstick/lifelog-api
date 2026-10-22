package authmigration

import (
	"lifelog-api/internal/authsvc"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	migrator := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		initialMigration(),
		normalizeLoginsMigration(),
		subscriptionsMigration(),
		emailMigration(),
		blocksMigration(),
		devicesMigration(),
		rateCountersMigration(),
		widenEmailCodeKeyMigration(),
		dropMergedIntoMigration(),
		visitsMigration(),
	})
	return migrator.Migrate()
}

func initialMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_init",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(
				&authsvc.User{},
				&authsvc.Password{},
				&authsvc.TelegramLink{},
				&authsvc.Session{},
				&authsvc.LinkNonce{},
				&authsvc.Merge{},
			); err != nil {
				return err
			}

			statements := []string{
				`ALTER TABLE auth_users ADD COLUMN IF NOT EXISTS merged_into uuid`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_login
					ON auth_users (login) WHERE login IS NOT NULL AND merged_into IS NULL`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_telegram_links_telegram_id
					ON auth_telegram_links (telegram_id)`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_sessions_refresh_hash
					ON auth_sessions (refresh_hash)`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable(
				"auth_merges",
				"auth_link_nonces",
				"auth_sessions",
				"auth_telegram_links",
				"auth_passwords",
				"auth_users",
			)
		},
	}
}
