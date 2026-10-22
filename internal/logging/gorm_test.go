package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line is not JSON: %q: %v", line, err)
		}
		out = append(out, record)
	}
	return out
}

func query() (string, int64) { return "SELECT 1", 3 }

func TestGormTraceSlowQueryIsOneWarnWithFields(t *testing.T) {
	logger, buf := captureLogger()
	gl := NewGormLogger(logger, 10*time.Millisecond)

	gl.Trace(context.Background(), time.Now().Add(-50*time.Millisecond), query, nil)

	got := records(t, buf)
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d: %s", len(got), buf)
	}
	r := got[0]
	if r["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", r["level"])
	}
	if r["sql"] != "SELECT 1" {
		t.Errorf("sql = %v", r["sql"])
	}
	if r["rows"] != float64(3) {
		t.Errorf("rows = %v", r["rows"])
	}
	if d, ok := r["duration_ms"].(float64); !ok || d < 50 {
		t.Errorf("duration_ms = %v", r["duration_ms"])
	}
	if msg, _ := r["msg"].(string); strings.Contains(msg, "SELECT") || strings.Contains(msg, "WARN") {
		t.Errorf("attributes leaked into msg: %q", msg)
	}
}

func TestGormTraceRecordNotFoundIsSilent(t *testing.T) {
	logger, buf := captureLogger()
	gl := NewGormLogger(logger, time.Second)

	gl.Trace(context.Background(), time.Now(), query, gorm.ErrRecordNotFound)

	if buf.Len() != 0 {
		t.Fatalf("want nothing, got %s", buf)
	}
}

func TestGormTraceFastSuccessIsSilent(t *testing.T) {
	logger, buf := captureLogger()
	NewGormLogger(logger, time.Second).Trace(context.Background(), time.Now(), query, nil)
	if buf.Len() != 0 {
		t.Fatalf("want nothing, got %s", buf)
	}
}

func TestGormTraceErrorIsError(t *testing.T) {
	logger, buf := captureLogger()
	gl := NewGormLogger(logger, time.Second)

	gl.Trace(context.Background(), time.Now(), query, errors.New("boom"))

	got := records(t, buf)
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0]["level"] != "ERROR" || got[0]["error"] != "boom" || got[0]["sql"] != "SELECT 1" {
		t.Errorf("unexpected record: %v", got[0])
	}
}

func TestGormNilLoggerFollowsDefaultSetLater(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	gl := NewGormLogger(nil, 10*time.Millisecond)
	logger, buf := captureLogger()
	slog.SetDefault(logger)

	gl.Trace(context.Background(), time.Now().Add(-50*time.Millisecond), query, nil)

	got := records(t, buf)
	if len(got) != 1 || got[0]["level"] != "WARN" {
		t.Fatalf("want one WARN record through the new default, got %s", buf)
	}
}

func TestGormLogNeverCarriesBoundValues(t *testing.T) {
	logger, buf := captureLogger()

	gl := NewGormLogger(logger, time.Hour)
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 user=x dbname=x sslmode=disable"}), &gorm.Config{
		Logger:               gl,
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	type secretRow struct {
		ID    uint
		Email string
		Hash  string
	}
	if err := db.Callback().Query().Register("test:fail", func(d *gorm.DB) { d.AddError(errors.New("boom")) }); err != nil {
		t.Fatal(err)
	}
	db.Where("email = ? AND hash = ?", "victim@example.com", "argon2-secret").Find(&[]secretRow{})

	out := buf.String()
	if strings.Contains(out, "victim@example.com") || strings.Contains(out, "argon2-secret") {
		t.Fatalf("bound values leaked into the log: %s", out)
	}
	if !strings.Contains(out, "email = $1 AND hash = $2") || !strings.Contains(out, "secret_rows") {
		t.Fatalf("SQL shape lost from the log: %s", out)
	}
}
