package notesmigration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	options := *gormigrate.DefaultOptions
	options.TableName = "notes_migrations"
	migrator := gormigrate.New(db, &options, []*gormigrate.Migration{
		initialMigration(),
	})
	return migrator.Migrate()
}

func initialMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "notes_init",
		Migrate: func(db *gorm.DB) error {
			statements := []string{
				`CREATE TABLE notes (
					id         bigserial PRIMARY KEY,
					user_id    uuid         NOT NULL,
					title      varchar(120),
					body       text         NOT NULL,
					day        date,
					created_at timestamptz  NOT NULL DEFAULT now(),
					updated_at timestamptz  NOT NULL DEFAULT now()
				)`,
				`CREATE INDEX idx_notes_user_order
					ON notes (user_id, day DESC NULLS LAST, updated_at DESC, id DESC)`,

				`CREATE TABLE note_tags (
					note_id bigint      NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
					user_id uuid        NOT NULL,
					tag     varchar(30) NOT NULL,
					PRIMARY KEY (note_id, tag)
				)`,
				`CREATE INDEX idx_note_tags_user_tag ON note_tags (user_id, tag, note_id)`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP TABLE IF EXISTS note_tags, notes`).Error
		},
	}
}
