package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDSNQuotesValues(t *testing.T) {
	cfg := PgConfig{Host: "db", Port: "5432", User: "app", Database: "lifelog", Password: `p a's\s\`}
	parsed, err := pgconn.ParseConfig(cfg.dsn())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Password != `p a's\s\` || parsed.User != "app" || parsed.Database != "lifelog" || parsed.Host != "db" || parsed.Port != 5432 {
		t.Fatalf("got %+v", parsed)
	}
}
