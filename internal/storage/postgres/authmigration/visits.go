package authmigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func visitsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_visits",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS visit_salts (
					day date PRIMARY KEY,
					salt bytea NOT NULL
				);
				CREATE TABLE IF NOT EXISTS visit_days (
					day date NOT NULL,
					visitor text NOT NULL,
					first_seen timestamptz NOT NULL,
					last_seen timestamptz NOT NULL,
					requests integer NOT NULL,
					user_agent text NOT NULL DEFAULT '',
					PRIMARY KEY (day, visitor)
				);
				CREATE TABLE IF NOT EXISTS visit_requests (
					id bigserial PRIMARY KEY,
					day date NOT NULL,
					visitor text NOT NULL,
					at timestamptz NOT NULL,
					service text NOT NULL,
					method text NOT NULL,
					path text NOT NULL,
					status integer NOT NULL,
					duration_ms real NOT NULL
				);
				CREATE INDEX IF NOT EXISTS visit_requests_visitor ON visit_requests (day, visitor, at);
			`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP TABLE IF EXISTS visit_requests; DROP TABLE IF EXISTS visit_days; DROP TABLE IF EXISTS visit_salts`).Error
		},
	}
}
