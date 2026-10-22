package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type bodyProbe struct {
	called  bool
	read    int
	tooLong bool
}

func newProbeEngine(t *testing.T, limit int64) (*gin.Engine, *bodyProbe) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := NewEngine(EngineOptions{
		Service:      "test",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Health:       func(c *gin.Context) { c.Status(http.StatusOK) },
		MaxBodyBytes: limit,
	})
	probe := &bodyProbe{}
	engine.POST("/echo", func(c *gin.Context) {
		probe.called = true
		data, err := io.ReadAll(c.Request.Body)
		probe.read = len(data)
		var tooLarge *http.MaxBytesError
		probe.tooLong = errors.As(err, &tooLarge)
		c.Status(http.StatusNoContent)
	})
	return engine, probe
}

func post(engine *gin.Engine, body io.Reader, contentType string, length int64) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/echo", body)
	request.Header.Set("Content-Type", contentType)
	if length != 0 {
		request.ContentLength = length
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func assertTooLarge(t *testing.T, recorder *httptest.ResponseRecorder, probe *bodyProbe) {
	t.Helper()
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", recorder.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error != BodyTooLargeText {
		t.Fatalf("body %q, want error %q", recorder.Body.String(), BodyTooLargeText)
	}
	if probe.called {
		t.Fatal("handler ran for a body rejected by its declared length")
	}
}

func TestLimitBodyRejectsDeclaredLengthOverLimit(t *testing.T) {
	engine, probe := newProbeEngine(t, 64)
	assertTooLarge(t, post(engine, strings.NewReader(strings.Repeat("a", 65)), "application/json", 0), probe)
}

func TestLimitBodyAcceptsBodyAtLimit(t *testing.T) {
	engine, probe := newProbeEngine(t, 64)
	recorder := post(engine, strings.NewReader(strings.Repeat("a", 64)), "application/json", 0)
	if recorder.Code != http.StatusNoContent || probe.read != 64 || probe.tooLong {
		t.Fatalf("status %d, read %d, tooLong %v; want 204 with the whole body", recorder.Code, probe.read, probe.tooLong)
	}
}

func TestLimitBodyCutsChunkedBody(t *testing.T) {
	engine, probe := newProbeEngine(t, 64)
	post(engine, io.NopCloser(strings.NewReader(strings.Repeat("a", 1000))), "application/json", -1)
	if !probe.called || !probe.tooLong || probe.read > 64 {
		t.Fatalf("called %v, read %d, tooLong %v; want the read cut at 64 bytes", probe.called, probe.read, probe.tooLong)
	}
}

func TestLimitBodySkipsMultipart(t *testing.T) {
	engine, probe := newProbeEngine(t, 64)
	recorder := post(engine, strings.NewReader(strings.Repeat("a", 1000)), "multipart/form-data; boundary=x", 0)
	if recorder.Code != http.StatusNoContent || probe.read != 1000 || probe.tooLong {
		t.Fatalf("status %d, read %d, tooLong %v; want the whole multipart body", recorder.Code, probe.read, probe.tooLong)
	}
}

func TestLimitBodyZeroMeansDefault(t *testing.T) {
	engine, probe := newProbeEngine(t, 0)
	recorder := post(engine, strings.NewReader(strings.Repeat("a", DefaultMaxBodyBytes)), "application/json", 0)
	if recorder.Code != http.StatusNoContent || probe.read != DefaultMaxBodyBytes {
		t.Fatalf("status %d, read %d; want the default limit to admit 1 MB", recorder.Code, probe.read)
	}

	engine, probe = newProbeEngine(t, 0)
	assertTooLarge(t, post(engine, strings.NewReader(strings.Repeat("a", DefaultMaxBodyBytes+1)), "application/json", 0), probe)
}
