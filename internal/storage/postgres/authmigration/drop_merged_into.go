package authmigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func dropMergedIntoMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_drop_merged_into",
		Migrate: func(db *gorm.DB) error {
			const tombstones = `SELECT id FROM auth_users WHERE merged_into IS NOT NULL`
			return execAll(db, []string{
				`DELETE FROM auth_passwords WHERE user_id IN (` + tombstones + `)`,
				`DELETE FROM auth_telegram_links WHERE user_id IN (` + tombstones + `)`,
				`DELETE FROM auth_sessions WHERE user_id IN (` + tombstones + `)`,
				`DELETE FROM auth_link_nonces WHERE user_id IN (` + tombstones + `)`,
				`DELETE FROM auth_email_codes WHERE user_id IN (` + tombstones + `)`,
				`DELETE FROM auth_subscriptions WHERE follower_id IN (` + tombstones + `) OR target_id IN (` + tombstones + `)`,
				`DELETE FROM auth_blocks WHERE blocker_id IN (` + tombstones + `) OR blocked_id IN (` + tombstones + `)`,
				`DELETE FROM auth_users WHERE merged_into IS NOT NULL`,

				`DROP INDEX IF EXISTS idx_auth_users_login`,
				`DROP INDEX IF EXISTS idx_auth_users_email`,
				`ALTER TABLE auth_users DROP COLUMN IF EXISTS merged_into`,
				`CREATE UNIQUE INDEX idx_auth_users_login
					ON auth_users (lower(login)) WHERE login IS NOT NULL`,
				`CREATE UNIQUE INDEX idx_auth_users_email
					ON auth_users (lower(email)) WHERE email IS NOT NULL`,
			})
		},

		Rollback: func(db *gorm.DB) error {
			return execAll(db, []string{
				`ALTER TABLE auth_users ADD COLUMN IF NOT EXISTS merged_into uuid`,
				`CREATE INDEX IF NOT EXISTS idx_auth_users_merged_into ON auth_users (merged_into)`,
				`DROP INDEX IF EXISTS idx_auth_users_login`,
				`DROP INDEX IF EXISTS idx_auth_users_email`,
				`CREATE UNIQUE INDEX idx_auth_users_login
					ON auth_users (lower(login)) WHERE login IS NOT NULL AND merged_into IS NULL`,
				`CREATE UNIQUE INDEX idx_auth_users_email
					ON auth_users (lower(email)) WHERE email IS NOT NULL AND merged_into IS NULL`,
			})
		},
	}
}

func execAll(db *gorm.DB, statements []string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
