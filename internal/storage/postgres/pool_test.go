package postgres

import (
	"database/sql"
	"testing"
)

func TestConfigurePool(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", "host=127.0.0.1 port=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	configurePool(db, servicePoolSize)

	if got := db.Stats().MaxOpenConnections; got != servicePoolSize {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, servicePoolSize)
	}
}
