package notifymigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	options := *gormigrate.DefaultOptions
	options.TableName = "notify_migrations"
	migrator := gormigrate.New(db, &options, []*gormigrate.Migration{
		initialMigration(),
		webPushEndpointMigration(),
	})
	return migrator.Migrate()
}

func initialMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "notify_init",
		Migrate: func(db *gorm.DB) error {
			statements := []string{
				`CREATE TABLE push_devices (
					id           uuid        PRIMARY KEY,
					user_id      uuid        NOT NULL,
					device_id    text        NOT NULL,
					channel      text        NOT NULL,
					token        text,
					endpoint     text,
					p256dh       text,
					auth         text,
					platform     text        NOT NULL DEFAULT '',
					label        text        NOT NULL DEFAULT '',
					enabled      boolean     NOT NULL DEFAULT true,
					created_at   timestamptz NOT NULL DEFAULT now(),
					updated_at   timestamptz NOT NULL DEFAULT now(),
					last_seen_at timestamptz NOT NULL DEFAULT now(),
					last_error   text,
					CONSTRAINT push_devices_user_device UNIQUE (user_id, device_id)
				)`,

				`CREATE UNIQUE INDEX push_devices_token ON push_devices (token) WHERE token IS NOT NULL`,

				`CREATE TABLE push_settings (
					user_id      uuid        PRIMARY KEY,
					push_enabled boolean     NOT NULL DEFAULT true,
					updated_at   timestamptz NOT NULL DEFAULT now()
				)`,

				`CREATE TABLE push_outbox (
					id              bigserial   PRIMARY KEY,
					device_id       uuid        NOT NULL REFERENCES push_devices (id) ON DELETE CASCADE,
					payload         jsonb       NOT NULL,
					attempts        integer     NOT NULL DEFAULT 0,
					next_attempt_at timestamptz NOT NULL DEFAULT now(),
					created_at      timestamptz NOT NULL DEFAULT now()
				)`,
				`CREATE INDEX push_outbox_due ON push_outbox (next_attempt_at, id)`,
				`CREATE INDEX push_outbox_device ON push_outbox (device_id)`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP TABLE IF EXISTS push_outbox, push_settings, push_devices`).Error
		},
	}
}

func webPushEndpointMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "notify_webpush_endpoint",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`CREATE UNIQUE INDEX push_devices_endpoint ON push_devices (endpoint) WHERE endpoint IS NOT NULL`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP INDEX IF EXISTS push_devices_endpoint`).Error
		},
	}
}
