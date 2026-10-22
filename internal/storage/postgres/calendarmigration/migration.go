package calendarmigration

import (
	"fmt"

	"lifelog-api/internal/calendar"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	migrator := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		initialMigration(),
		googleImportMigration(),
		eventTimezoneMigration(),
		singleNotificationSwitchMigration(),
		googleAllDayUTCMigration(),
	})
	return migrator.Migrate()
}

func googleAllDayUTCMigration() *gormigrate.Migration {
	const toLocalDate = `date_trunc('day', (%[1]s AT TIME ZONE 'UTC') + interval '13 hours') AT TIME ZONE 'UTC'`
	return &gormigrate.Migration{
		ID: "calendar_google_all_day_utc",
		Migrate: func(db *gorm.DB) error {
			for _, column := range []string{"starts_at", "ends_at"} {
				if err := db.Exec(fmt.Sprintf(`UPDATE calendar_imported_events
					SET %[1]s = `+toLocalDate+`
					WHERE all_day AND %[1]s IS NOT NULL
						AND (%[1]s AT TIME ZONE 'UTC')::time <> '00:00'`, column)).Error; err != nil {
					return err
				}
			}
			return nil
		},

		Rollback: func(*gorm.DB) error { return nil },
	}
}

func initialMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "calendar_init",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(
				&calendar.Event{},
				&calendar.Settings{},
				&calendar.Delivery{},
			); err != nil {
				return err
			}

			if err := createPreferences(db); err != nil {
				return err
			}

			if err := db.Exec(`ALTER TABLE calendar_events
				DROP CONSTRAINT IF EXISTS chk_calendar_event_span`).Error; err != nil {
				return err
			}
			if err := db.Exec(`ALTER TABLE calendar_events
				ADD CONSTRAINT chk_calendar_event_span
				CHECK (ends_at IS NULL OR ends_at >= starts_at)`).Error; err != nil {
				return err
			}

			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable(
				"calendar_deliveries",
				"calendar_settings",
				"calendar_preferences",
				"calendar_events",
			)
		},
	}
}

func googleImportMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "calendar_google_import",
		Migrate: func(db *gorm.DB) error {
			return db.AutoMigrate(
				&calendar.GoogleLink{},
				&calendar.GoogleState{},
				&calendar.ImportedEvent{},
			)
		},
		Rollback: func(db *gorm.DB) error {
			return db.Migrator().DropTable(
				"calendar_imported_events",
				"calendar_google_states",
				"calendar_google_links",
			)
		},
	}
}

func createPreferences(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS calendar_preferences (
		user_id uuid NOT NULL,
		kind varchar(32) NOT NULL,
		digest boolean NOT NULL DEFAULT true,
		lead_minutes bigint NOT NULL DEFAULT 0,
		created_at timestamptz,
		updated_at timestamptz,
		PRIMARY KEY (user_id, kind),
		CONSTRAINT chk_calendar_pref_lead CHECK (lead_minutes >= 0 AND lead_minutes <= 43200)
	)`).Error
}

func eventTimezoneMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "calendar_event_timezone",
		Migrate: func(db *gorm.DB) error {
			return db.Exec(`ALTER TABLE calendar_events
				ADD COLUMN IF NOT EXISTS timezone varchar(64)`).Error
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`ALTER TABLE calendar_events DROP COLUMN IF EXISTS timezone`).Error
		},
	}
}

func singleNotificationSwitchMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "calendar_single_switch",
		Migrate: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE calendar_settings
				ADD COLUMN IF NOT EXISTS lead_minutes bigint NOT NULL DEFAULT 30`).Error; err != nil {
				return err
			}
			if err := db.Exec(`UPDATE calendar_settings s
				SET lead_minutes = p.lead_minutes
				FROM calendar_preferences p
				WHERE p.user_id = s.user_id AND p.kind = 'own_event'`).Error; err != nil {
				return err
			}
			if err := db.Exec(`ALTER TABLE calendar_settings
				DROP CONSTRAINT IF EXISTS chk_calendar_settings_lead`).Error; err != nil {
				return err
			}
			if err := db.Exec(`ALTER TABLE calendar_settings
				ADD CONSTRAINT chk_calendar_settings_lead
				CHECK (lead_minutes >= 0 AND lead_minutes <= 43200)`).Error; err != nil {
				return err
			}
			return db.Migrator().DropTable("calendar_preferences")
		},
		Rollback: func(db *gorm.DB) error {
			if err := createPreferences(db); err != nil {
				return err
			}
			if err := db.Exec(`ALTER TABLE calendar_settings
				DROP CONSTRAINT IF EXISTS chk_calendar_settings_lead`).Error; err != nil {
				return err
			}
			return db.Exec(`ALTER TABLE calendar_settings
				DROP COLUMN IF EXISTS lead_minutes`).Error
		},
	}
}
