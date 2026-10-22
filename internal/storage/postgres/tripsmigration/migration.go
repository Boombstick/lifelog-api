package tripsmigration

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

const (
	BaselineID = "trips-0001-baseline"

	OfflineSyncID = "trips-0002-offline-sync"

	OneActiveTripID = "trips-0003-one-active-trip"

	SnakeCaseSyncResultsID = "trips-0004-snake-case-sync-results"

	RestrictExpenseFKsID = "trips-0005-restrict-expense-fks"
)

const ActiveTripIndex = "ux_trips_user_id_active"

func Migrate(db *gorm.DB) error {
	return newMigrator(db).Migrate()
}

func newMigrator(db *gorm.DB) *gormigrate.Gormigrate {
	options := *gormigrate.DefaultOptions
	options.TableName = "trips_migrations"
	return gormigrate.New(db, &options, []*gormigrate.Migration{
		baseline(),
		offlineSync(),
		oneActiveTrip(),
		snakeCaseSyncResults(),
		restrictExpenseFKs(),
	})
}

func execAll(db *gorm.DB, statements ...string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range statements {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("%w\nstatement: %s", err, firstLine(stmt))
			}
		}
		return nil
	})
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func scalarBool(db *gorm.DB, query string) (bool, error) {
	var v bool
	err := db.Raw(query).Row().Scan(&v)
	return v, err
}

func baseline() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: BaselineID,
		Migrate: func(db *gorm.DB) error {
			exists, err := scalarBool(db, `SELECT to_regclass('public.trips') IS NOT NULL`)
			if err != nil {
				return err
			}
			if exists {
				return nil
			}
			return execAll(db, prodSchema...)
		},

		Rollback: func(db *gorm.DB) error {
			return errors.New("tripsmigration: the baseline is not rolled back automatically; take a dump and drop the tables by hand")
		},
	}
}

func offlineSync() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: OfflineSyncID,
		Migrate: func(db *gorm.DB) error {
			applied, err := scalarBool(db, `
				SELECT EXISTS (SELECT 1 FROM information_schema.columns
				               WHERE table_schema = 'public' AND table_name = 'expenses' AND column_name = 'client_id')
				    OR (to_regclass('public."__EFMigrationsHistory"') IS NOT NULL
				        AND EXISTS (SELECT 1 FROM public."__EFMigrationsHistory"
				                    WHERE migration_id = '20260916174621_OfflineSync'))`)
			if err != nil {
				return err
			}
			if applied {
				return nil
			}
			return execAll(db, offlineSyncUp...)
		},
		Rollback: func(db *gorm.DB) error {
			return execAll(db, offlineSyncDown...)
		},
	}
}

func oneActiveTrip() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: OneActiveTripID,
		Migrate: func(db *gorm.DB) error {
			var duplicates []struct {
				UserID string
				Trips  int
			}
			if err := db.Raw(`SELECT user_id::text AS user_id, COUNT(*) AS trips
			                  FROM trips WHERE status = 2
			                  GROUP BY user_id HAVING COUNT(*) > 1
			                  ORDER BY user_id`).Scan(&duplicates).Error; err != nil {
				return err
			}
			if len(duplicates) > 0 {
				parts := make([]string, len(duplicates))
				for i, d := range duplicates {
					parts[i] = fmt.Sprintf("%s (%d active)", d.UserID, d.Trips)
				}
				return fmt.Errorf("tripsmigration: cannot create %s: users with more than one active trip: %s; "+
					"complete the extra trips (status = 3) and restart", ActiveTripIndex, strings.Join(parts, ", "))
			}
			return execAll(db, `CREATE UNIQUE INDEX IF NOT EXISTS `+ActiveTripIndex+` ON trips (user_id) WHERE status = 2`)
		},
		Rollback: func(db *gorm.DB) error {
			return execAll(db, `DROP INDEX IF EXISTS `+ActiveTripIndex)
		},
	}
}

func snakeCaseSyncResults() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: SnakeCaseSyncResultsID,
		Migrate: func(db *gorm.DB) error {
			exists, err := scalarBool(db, `SELECT to_regclass('public.sync_operations') IS NOT NULL`)
			if err != nil || !exists {
				return err
			}
			return execAll(db, `DELETE FROM sync_operations WHERE NOT jsonb_exists(result_json, 'op_id')`)
		},

		Rollback: func(*gorm.DB) error { return nil },
	}
}

const (
	fkExpenseSnapshot = "fk_expenses_exchange_rate_snapshots_exchange_rates_snapshot_id"
	fkExpenseCurrency = "fk_expenses_currencies_currency_code"
)

func restrictExpenseFKs() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: RestrictExpenseFKsID,
		Migrate: func(db *gorm.DB) error {
			return execAll(db, expenseFKStatements("RESTRICT")...)
		},
		Rollback: func(db *gorm.DB) error {
			return execAll(db, expenseFKStatements("CASCADE")...)
		},
	}
}

func expenseFKStatements(onDelete string) []string {
	return []string{
		`ALTER TABLE expenses DROP CONSTRAINT IF EXISTS ` + fkExpenseSnapshot,
		`ALTER TABLE expenses ADD CONSTRAINT ` + fkExpenseSnapshot + `
    FOREIGN KEY (exchange_rates_snapshot_id) REFERENCES exchange_rate_snapshots(id) ON DELETE ` + onDelete,
		`ALTER TABLE expenses DROP CONSTRAINT IF EXISTS ` + fkExpenseCurrency,
		`ALTER TABLE expenses ADD CONSTRAINT ` + fkExpenseCurrency + `
    FOREIGN KEY (currency_code) REFERENCES currencies(code) ON DELETE ` + onDelete,
	}
}
