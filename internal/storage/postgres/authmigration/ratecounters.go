package authmigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func rateCountersMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "auth_rate_counters",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`CREATE TABLE IF NOT EXISTS auth_rate_counters (
				key text PRIMARY KEY,
				hits integer NOT NULL,
				window_start timestamptz NOT NULL
			)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP TABLE IF EXISTS auth_rate_counters`).Error
		},
	}
}
