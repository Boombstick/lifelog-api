package authmigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func normalizeLoginsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_normalize_logins",
		Migrate: func(db *gorm.DB) error {
			statements := []string{
				`UPDATE auth_users SET login = lower(btrim(login))
					WHERE login IS NOT NULL AND login <> lower(btrim(login))`,
				`DROP INDEX IF EXISTS idx_auth_users_login`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_login
					ON auth_users (lower(login)) WHERE login IS NOT NULL AND merged_into IS NULL`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`DROP INDEX IF EXISTS idx_auth_users_login`).Error; err != nil {
				return err
			}
			return db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_login
				ON auth_users (login) WHERE login IS NOT NULL AND merged_into IS NULL`).Error
		},
	}
}
