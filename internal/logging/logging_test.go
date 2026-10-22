package logging

import (
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRecoveryLogsOneErrorRecordWithStack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger, buf := captureLogger()

	engine := gin.New()
	engine.Use(Recovery(logger, nil))
	engine.GET("/boom", func(*gin.Context) { panic("kaboom") })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	got := records(t, buf)
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d: %s", len(got), buf)
	}
	r := got[0]
	if r["level"] != "ERROR" || r["panic"] != "kaboom" || r["path"] != "/boom" {
		t.Errorf("unexpected record: %v", r)
	}
	if stack, _ := r["stack"].(string); !strings.Contains(stack, "goroutine") {
		t.Errorf("stack missing: %q", stack)
	}
}

func TestRecoveryUsesRespond(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger, _ := captureLogger()

	engine := gin.New()
	engine.Use(Recovery(logger, func(c *gin.Context, _ any) {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal"})
	}))
	engine.GET("/boom", func(*gin.Context) { panic("kaboom") })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"error":"internal"`) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestWriterBridgesStdLog(t *testing.T) {
	logger, buf := captureLogger()
	std := log.New(Writer(logger, slog.LevelWarn), "", 0)

	std.Printf("http: TLS handshake error from %s", "1.2.3.4:5")

	got := records(t, buf)
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0]["level"] != "WARN" || got[0]["msg"] != "http: TLS handshake error from 1.2.3.4:5" {
		t.Errorf("unexpected record: %v", got[0])
	}
}

func TestRequestsRedactsShareTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger, buf := captureLogger()

	engine := gin.New()
	engine.Use(Requests(logger))
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	engine.GET("/s/:token", ok)
	engine.GET("/api/v1/shared/:token", ok)
	engine.DELETE("/api/v1/me/share-links/:token", ok)
	engine.GET("/api/v1/films/:id", ok)

	const token = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/s/"+token, nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/shared/"+token, nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/me/share-links/"+token, nil),
	} {
		engine.ServeHTTP(httptest.NewRecorder(), req)
	}
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/films/42", nil))

	if strings.Contains(buf.String(), token) || strings.Contains(buf.String(), token[:10]) {
		t.Fatalf("share token leaked: %s", buf)
	}
	got := records(t, buf)
	if len(got) != 4 {
		t.Fatalf("want 4 records, got %d", len(got))
	}
	if got[0]["path"] != "/s/AbCd…" {
		t.Errorf("path = %v", got[0]["path"])
	}
	if got[3]["path"] != "/api/v1/films/42" {
		t.Errorf("ordinary path changed: %v", got[3]["path"])
	}
}
