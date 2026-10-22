package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"
	"lifelog-api/internal/service"
	"lifelog-api/internal/tmdbadd"

	"github.com/gin-gonic/gin"
)

type slowAdder struct{ delay time.Duration }

func (s slowAdder) Search(context.Context, string, int) (tmdbadd.SearchResponse, error) {
	return tmdbadd.SearchResponse{}, nil
}

func (s slowAdder) Add(context.Context, uint, film.Kind, int) (tmdbadd.AddResponse, error) {
	time.Sleep(s.delay)
	return tmdbadd.AddResponse{ID: 5, Created: true}, nil
}

func TestTMDBAddOutlastsServerWriteTimeout(t *testing.T) {
	t.Parallel()

	const delay = 300 * time.Millisecond
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.ContextWithFallback = true
	signedIn := func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithPrincipal(c.Request.Context(), auth.Principal{UserID: 7}))
		c.Next()
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	NewTMDBAddHandler(logger, slowAdder{delay: delay}).Register(engine.Group("/api/v1"), signedIn)
	engine.POST("/plain", func(c *gin.Context) {
		time.Sleep(delay)
		c.JSON(http.StatusCreated, gin.H{"id": 5})
	})

	server := httptest.NewUnstartedServer(engine)
	server.Config.WriteTimeout = 50 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)

	post := func(path string) (*http.Response, error) {
		return server.Client().Post(server.URL+path, "application/json",
			strings.NewReader(`{"kind":"series","tmdb_id":1396}`))
	}

	if resp, err := post("/plain"); err == nil {
		resp.Body.Close()
		t.Fatalf("control route answered %d past WriteTimeout; the test proves nothing", resp.StatusCode)
	}

	resp, err := post("/api/v1/tmdb/add")
	if err != nil {
		t.Fatalf("add answer lost: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated || !strings.Contains(string(body), `"id":5`) {
		t.Errorf("got %d %s", resp.StatusCode, body)
	}
}

func TestSearchUnavailableAnswers(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cases := []struct {
		name       string
		err        error
		cancelled  bool
		wantStatus int
		wantRetry  bool
	}{
		{"read deadline", fmt.Errorf("wrapped: %w", service.ErrSearchTimeout), false, http.StatusServiceUnavailable, true},
		{"overloaded", service.ErrSearchOverloaded, false, http.StatusServiceUnavailable, true},
		{"broken", errors.New("search: 500"), false, http.StatusBadGateway, false},

		{"client gone", service.ErrSearchTimeout, true, statusClientClosed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			ctx, cancel := context.WithCancel(context.Background())
			if tc.cancelled {
				cancel()
			} else {
				defer cancel()
			}
			c.Request = httptest.NewRequestWithContext(ctx, http.MethodGet, "/search", nil)

			searchUnavailable(c, logger, "log", "поиск временно недоступен", tc.err)

			if recorder.Code != tc.wantStatus {
				t.Errorf("status %d, want %d", recorder.Code, tc.wantStatus)
			}
			if got := recorder.Header().Get("Retry-After") == "1"; got != tc.wantRetry {
				t.Errorf("Retry-After %q", recorder.Header().Get("Retry-After"))
			}
			if tc.wantStatus == http.StatusServiceUnavailable && !strings.Contains(recorder.Body.String(), "поиск временно недоступен") {
				t.Errorf("body %s", recorder.Body.String())
			}
		})
	}
}
