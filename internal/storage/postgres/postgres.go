package postgres

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lifelog-api/internal/config"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/storage/postgres/migration"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type PgConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string

	Logger *slog.Logger
}

const slowQuery = 200 * time.Millisecond

func (c *PgConfig) gormConfig() *gorm.Config {
	return &gorm.Config{Logger: logging.NewGormLogger(c.Logger, slowQuery)}
}

const (
	catalogPoolSize = 20
	servicePoolSize = 10

	poolMaxIdleTime = 5 * time.Minute

	poolMaxLifetime = 30 * time.Minute
)

func configurePool(db *sql.DB, poolSize int) {
	db.SetMaxOpenConns(poolSize)
	db.SetMaxIdleConns(poolSize)
	db.SetConnMaxIdleTime(poolMaxIdleTime)
	db.SetConnMaxLifetime(poolMaxLifetime)
}

func NewPostgres(cfg *PgConfig) (*gorm.DB, error) {
	return open(cfg, catalogPoolSize, "catalog", Migrate)
}

func open(cfg *PgConfig, poolSize int, name string, migrate func(*gorm.DB) error) (*gorm.DB, error) {
	gormDB, err := gorm.Open(postgres.Open(cfg.dsn()), cfg.gormConfig())
	if err != nil {
		return nil, err
	}

	db, err := gormDB.DB()
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("SELECT 1"); err != nil {
		return nil, fmt.Errorf("could not reach the %s database: %w", name, err)
	}
	configurePool(db, poolSize)

	if err := migrate(gormDB); err != nil {
		return nil, fmt.Errorf("%s migrations: %w", name, err)
	}
	return gormDB, nil
}

func (c *PgConfig) dsn() string {
	quote := func(v string) string {
		v = strings.ReplaceAll(v, `\`, `\\`)
		v = strings.ReplaceAll(v, `'`, `\'`)
		return "'" + v + "'"
	}
	return fmt.Sprintf("host=%s user=%s dbname=%s sslmode=disable password=%s port=%s",
		quote(c.Host), quote(c.User), quote(c.Database), quote(c.Password), quote(c.Port))
}

func Migrate(db *gorm.DB) error {
	dbMigrator := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		migration.InitialMigration(),
		migration.AppendReviewToPlace(),
		migration.AddUserMigration(),
		migration.AddFilmDomainMigration(),
		migration.AlterFilmTMDBFieldsMigration(),
		migration.AddFilmRatingsMigration(),
		migration.AddGeoAndPlaceFieldsMigration(),
		migration.AddAuthUserIDMigration(),
		migration.DropUserCredentialsMigration(),
		migration.AddSharingMigration(),
		migration.AddShareLinksMigration(),
		migration.AddFilmRecommendationsMigration(),
		migration.AddPersonPhotoMigration(),
		migration.AddShareLinkExpiryMigration(),
		migration.AddPlaceReviewAuthorMigration(),
		migration.AddSeriesMigration(),
		migration.AddSeasonTrackingMigration(),
		migration.ReworkSeasonTrackingMigration(),
		migration.AddCollectionsMigration(),
		migration.AddFilmCreditsPersonIndexMigration(),
		migration.AddSharedListsMigration(),
		migration.AddImportJobsMigration(),
		migration.AddFilmsManualAddedMigration(),
		migration.AddPlaceAuthorMigration(),
		migration.DropSubscriptionsReplicaMigration(),
		migration.AddFilmEntriesFilmIndexMigration(),
	})
	return dbMigrator.Migrate()
}

func ConfigFrom(c *config.Config, database string, logger *slog.Logger) *PgConfig {
	return &PgConfig{
		Host:     c.PostgresHost,
		Port:     c.PostgresPort,
		User:     c.PostgresUser,
		Password: c.PostgresPassword,
		Database: database,
		Logger:   logger,
	}
}
