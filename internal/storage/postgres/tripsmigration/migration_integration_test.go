package tripsmigration

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testDSNEnv = "LIFELOG_TRIPS_TEST_PG"

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping Postgres integration test", testDSNEnv)
	}
	return dsn
}

func openDB(t *testing.T, dsn, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn+" dbname="+name), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func createDB(t *testing.T, dsn, name, template string) *gorm.DB {
	t.Helper()
	admin := openDB(t, dsn, "postgres")
	drop := func() {
		_ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`).Error
	}
	drop()
	stmt := `CREATE DATABASE "` + name + `"`
	if template != "" {
		stmt += ` TEMPLATE "` + template + `"`
	}
	if err := admin.Exec(stmt).Error; err != nil {
		if template != "" {
			t.Skipf("cannot copy %s (in use or missing?): %v", template, err)
		}
		t.Fatalf("create %s: %v", name, err)
	}
	db := openDB(t, dsn, name)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		drop()
	})
	return db
}

func schemaOf(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var lines []string
	collect := func(query string) {
		rows, err := db.Raw(query).Rows()
		if err != nil {
			t.Fatalf("schema query: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, s)
		}
	}
	collect(`SELECT concat_ws(' | ', 'column', table_name, column_name, data_type, udt_name,
	                character_maximum_length, numeric_precision, numeric_scale, is_nullable,
	                column_default, is_identity, identity_generation)
	         FROM information_schema.columns
	         WHERE table_schema = 'public' AND table_name <> 'trips_migrations'`)
	collect(`SELECT concat_ws(' | ', 'constraint', conrelid::regclass::text, conname, pg_get_constraintdef(oid))
	         FROM pg_constraint
	         WHERE connamespace = 'public'::regnamespace AND conrelid::regclass::text <> 'trips_migrations'`)
	collect(`SELECT concat_ws(' | ', 'index', tablename, indexname, indexdef)
	         FROM pg_indexes WHERE schemaname = 'public' AND tablename <> 'trips_migrations'`)
	collect(`SELECT concat_ws(' | ', 'sequence', sequencename, data_type, start_value, increment_by)
	         FROM pg_sequences WHERE schemaname = 'public'`)
	sort.Strings(lines)
	return lines
}

func diffSchemas(want, got []string) string {
	in := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	w, g := in(want), in(got)
	var b strings.Builder
	for _, s := range want {
		if !g[s] {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	for _, s := range got {
		if !w[s] {
			fmt.Fprintf(&b, "+ %s\n", s)
		}
	}
	return b.String()
}

func sourceDB() string {
	if v := os.Getenv("LIFELOG_TRIPS_TEST_SOURCE_DB"); v != "" {
		return v
	}
	return "lifelog"
}

func migratedEmptySchema(t *testing.T, dsn string) []string {
	t.Helper()
	db := createDB(t, dsn, "lifelog_trips_m1test_ref", "")
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate empty: %v", err)
	}
	return schemaOf(t, db)
}

func countRows(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM ` + table).Row().Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigrateEmptyDatabase(t *testing.T) {
	dsn := testDSN(t)
	db := createDB(t, dsn, "lifelog_trips_m1test", "")

	if err := Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	first := schemaOf(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if d := diffSchemas(first, schemaOf(t, db)); d != "" {
		t.Fatalf("second migrate changed the schema:\n%s", d)
	}

	for _, want := range []string{
		"index | trips | " + ActiveTripIndex + " | CREATE UNIQUE INDEX " + ActiveTripIndex + " ON public.trips USING btree (user_id) WHERE (status = 2)",
		"constraint | expenses | fk_expenses_expenses_duplicate_of_id | FOREIGN KEY (duplicate_of_id) REFERENCES expenses(id) ON DELETE SET NULL",
		"sequence | ExpenseCategories_Id_seq | bigint | 1 | 1",
	} {
		found := false
		for _, line := range first {
			if line == want {
				found = true
			}
		}
		if !found {
			t.Errorf("schema lacks %q", want)
		}
	}

	var applied []string
	if err := db.Raw(`SELECT id FROM trips_migrations ORDER BY id`).Scan(&applied).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Join(applied, ",") != BaselineID+","+OfflineSyncID+","+OneActiveTripID+","+SnakeCaseSyncResultsID+","+RestrictExpenseFKsID {
		t.Fatalf("history = %v", applied)
	}

	for _, fk := range []string{fkExpenseSnapshot, fkExpenseCurrency} {
		var onDelete string
		if err := db.Raw(`SELECT confdeltype::text FROM pg_constraint WHERE conname = ?`, fk).Row().Scan(&onDelete); err != nil {
			t.Fatalf("%s: %v", fk, err)
		}
		if onDelete != "r" {
			t.Errorf("%s: ON DELETE = %q, want RESTRICT (r)", fk, onDelete)
		}
	}

	m := newMigrator(db)
	if err := m.RollbackTo(BaselineID); err != nil {
		t.Fatalf("rollback to baseline: %v", err)
	}
	var syncTable *string
	if err := db.Raw(`SELECT to_regclass('public.sync_operations')::text`).Row().Scan(&syncTable); err != nil {
		t.Fatal(err)
	}
	if syncTable != nil {
		t.Errorf("sync_operations survived the rollback")
	}
	if err := newMigrator(db).RollbackLast(); err == nil {
		t.Errorf("baseline rollback should refuse")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate after rollback: %v", err)
	}
	if d := diffSchemas(first, schemaOf(t, db)); d != "" {
		t.Fatalf("re-migrate differs:\n%s", d)
	}
}

func TestMigrateExistingSyncSchema(t *testing.T) {
	dsn := testDSN(t)
	want := migratedEmptySchema(t, dsn)
	db := createDB(t, dsn, "lifelog_trips_m1test_copy", sourceDB())
	resetGoMigrations(t, db)

	counts := map[string]int64{}
	tables := []string{"users", "trips", "expenses", "trip_photos", "expense_categories", "currencies", "countries", "exchange_rate_snapshots", "sync_operations", "trip_client_ids"}
	for _, table := range tables {
		counts[table] = countRows(t, db, table)
	}

	counts["sync_operations"] = countRows(t, db, `sync_operations WHERE jsonb_exists(result_json, 'op_id')`)

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if d := diffSchemas(want, schemaOf(t, db)); d != "" {
		t.Fatalf("schema differs from a migrated empty database:\n%s", d)
	}
	for _, table := range tables {
		if got := countRows(t, db, table); got != counts[table] {
			t.Errorf("%s: %d rows, had %d", table, got, counts[table])
		}
	}
}

func TestMigrateExistingProdSchema(t *testing.T) {
	dsn := testDSN(t)
	want := migratedEmptySchema(t, dsn)
	db := createDB(t, dsn, "lifelog_trips_m1test_prod", sourceDB())
	resetGoMigrations(t, db)

	if err := execAll(db, offlineSyncDown...); err != nil {
		t.Fatalf("simulate prod: %v", err)
	}
	if err := db.Exec(`DELETE FROM "__EFMigrationsHistory" WHERE migration_id = '20260916174621_OfflineSync'`).Error; err != nil {
		t.Fatal(err)
	}
	trips := countRows(t, db, "trips")
	expenses := countRows(t, db, "expenses")

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if d := diffSchemas(want, schemaOf(t, db)); d != "" {
		t.Fatalf("schema differs from a migrated empty database:\n%s", d)
	}
	if got := countRows(t, db, "trips WHERE source = 1"); got != trips {
		t.Errorf("trips with source 1: %d of %d", got, trips)
	}
	if got := countRows(t, db, "expenses WHERE source = 1 AND client_id IS NULL"); got != expenses {
		t.Errorf("expenses backfilled: %d of %d", got, expenses)
	}
}

func resetGoMigrations(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := execAll(db, `DROP TABLE IF EXISTS trips_migrations`, `DROP INDEX IF EXISTS `+ActiveTripIndex); err != nil {
		t.Fatal(err)
	}
}

func TestOneActiveTripRefusesDuplicates(t *testing.T) {
	dsn := testDSN(t)
	db := createDB(t, dsn, "lifelog_trips_m1test_dup", "")
	if err := newMigrator(db).MigrateTo(OfflineSyncID); err != nil {
		t.Fatalf("migrate to 0002: %v", err)
	}

	userID := "01a0ab58-200b-7655-8000-000000000001"
	if err := execAll(db,
		`INSERT INTO users (id, username, created_at) VALUES ('`+userID+`', 'dup', now())`,
		`INSERT INTO trips (id, user_id, name, status, updated_at, created_at) VALUES
		   ('01a0ab58-200b-7655-8000-0000000000a1', '`+userID+`', 'one', 2, now(), now()),
		   ('01a0ab58-200b-7655-8000-0000000000a2', '`+userID+`', 'two', 2, now(), now())`,
	); err != nil {
		t.Fatal(err)
	}

	err := Migrate(db)
	if err == nil || !strings.Contains(err.Error(), userID+" (2 active)") {
		t.Fatalf("migrate with duplicates: %v", err)
	}

	if err := db.Exec(`UPDATE trips SET status = 3 WHERE name = 'two'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate after fixing: %v", err)
	}
	err = db.Exec(`UPDATE trips SET status = 2 WHERE name = 'two'`).Error
	if err == nil || !strings.Contains(err.Error(), ActiveTripIndex) {
		t.Fatalf("index does not reject a second active trip: %v", err)
	}
}
