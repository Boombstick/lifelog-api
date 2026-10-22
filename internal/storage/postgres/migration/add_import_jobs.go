package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddImportJobsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_import_jobs",
		Migrate: func(db *gorm.DB) error {
			statements := []string{
				`CREATE TABLE import_jobs (
					id               bigserial PRIMARY KEY,
					user_id          bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
					source           varchar(16) NOT NULL,
					kind             varchar(16) NOT NULL,
					status           varchar(16) NOT NULL,
					total            integer     NOT NULL DEFAULT 0,
					processed        integer     NOT NULL DEFAULT 0,
					added            integer     NOT NULL DEFAULT 0,
					skipped_existing integer     NOT NULL DEFAULT 0,
					not_found        integer     NOT NULL DEFAULT 0,
					unsupported      integer     NOT NULL DEFAULT 0,
					failed           integer     NOT NULL DEFAULT 0,
					catalog_added    integer     NOT NULL DEFAULT 0,
					error            text,
					not_found_items  jsonb       NOT NULL DEFAULT '[]'::jsonb,
					created_at       timestamptz NOT NULL DEFAULT now(),
					updated_at       timestamptz NOT NULL DEFAULT now(),
					finished_at      timestamptz
				)`,
				`CREATE INDEX idx_import_jobs_user ON import_jobs (user_id, id DESC)`,
				`CREATE UNIQUE INDEX idx_import_jobs_one_active ON import_jobs (user_id)
					WHERE status IN ('queued', 'running')`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP TABLE IF EXISTS import_jobs`).Error
		},
	}
}
