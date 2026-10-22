package migration

import (
	"lifelog-api/internal/film"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func AddCollectionsMigration() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "add_collections",
		Migrate: func(db *gorm.DB) error {
			if err := db.AutoMigrate(&film.Collection{}); err != nil {
				return err
			}

			if !db.Migrator().HasColumn(&film.Film{}, "collection_id") {
				if err := db.Migrator().AddColumn(&film.Film{}, "CollectionID"); err != nil {
					return err
				}
			}
			if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_films_collection_id
				ON films (collection_id) WHERE collection_id IS NOT NULL`).Error; err != nil {
				return err
			}

			return db.Exec(`ALTER TABLE films
				ADD CONSTRAINT fk_films_collections FOREIGN KEY (collection_id)
				REFERENCES collections (tmdb_id) ON DELETE SET NULL`).Error
		},
		Rollback: func(db *gorm.DB) error {
			if err := db.Exec(`ALTER TABLE films DROP CONSTRAINT IF EXISTS fk_films_collections`).Error; err != nil {
				return err
			}
			if err := db.Migrator().DropColumn(&film.Film{}, "CollectionID"); err != nil {
				return err
			}
			return db.Migrator().DropTable(&film.Collection{})
		},
	}
}
