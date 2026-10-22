package migration

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddSharedListsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_shared_lists",
		Migrate: func(db *gorm.DB) error {
			statements := []string{
				`CREATE TABLE shared_lists (
					id            bigserial PRIMARY KEY,
					owner_auth_id uuid        NOT NULL,
					title         varchar(80) NOT NULL,
					created_at    timestamptz NOT NULL DEFAULT now(),
					updated_at    timestamptz NOT NULL DEFAULT now()
				)`,
				`CREATE INDEX idx_shared_lists_owner ON shared_lists (owner_auth_id)`,

				`CREATE TABLE shared_list_members (
					list_id        bigint      NOT NULL REFERENCES shared_lists (id) ON DELETE CASCADE,
					member_auth_id uuid        NOT NULL,
					created_at     timestamptz NOT NULL DEFAULT now(),
					PRIMARY KEY (list_id, member_auth_id)
				)`,
				`CREATE INDEX idx_shared_list_members_member ON shared_list_members (member_auth_id)`,

				`CREATE TABLE shared_list_items (
					list_id          bigint      NOT NULL REFERENCES shared_lists (id) ON DELETE CASCADE,
					film_id          bigint      NOT NULL REFERENCES films (id) ON DELETE CASCADE,
					added_by_auth_id uuid        NOT NULL,
					added_at         timestamptz NOT NULL DEFAULT now(),
					PRIMARY KEY (list_id, film_id)
				)`,
				`CREATE INDEX idx_shared_list_items_film ON shared_list_items (film_id)`,
				`CREATE INDEX idx_shared_list_items_added_by ON shared_list_items (added_by_auth_id)`,

				`CREATE TABLE shared_list_draws (
					id               bigserial PRIMARY KEY,
					list_id          bigint      NOT NULL REFERENCES shared_lists (id) ON DELETE CASCADE,
					mode             varchar(8)  NOT NULL,
					count            smallint    NOT NULL,
					drawn_by_auth_id uuid        NOT NULL,
					created_at       timestamptz NOT NULL DEFAULT now()
				)`,
				`CREATE INDEX idx_shared_list_draws_list ON shared_list_draws (list_id, created_at DESC, id DESC)`,
				`CREATE INDEX idx_shared_list_draws_drawn_by ON shared_list_draws (drawn_by_auth_id)`,

				`CREATE TABLE shared_list_draw_films (
					draw_id  bigint   NOT NULL REFERENCES shared_list_draws (id) ON DELETE CASCADE,
					position smallint NOT NULL,
					film_id  bigint   NOT NULL REFERENCES films (id) ON DELETE CASCADE,
					PRIMARY KEY (draw_id, position)
				)`,
				`CREATE INDEX idx_shared_list_draw_films_film ON shared_list_draw_films (film_id)`,
			}
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(db *gorm.DB) error {
			return db.Exec(`DROP TABLE IF EXISTS shared_list_draw_films, shared_list_draws,
				shared_list_items, shared_list_members, shared_lists`).Error
		},
	}
}
