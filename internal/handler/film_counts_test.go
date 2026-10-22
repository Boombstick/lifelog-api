package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"
	"lifelog-api/internal/service"

	"github.com/gin-gonic/gin"
)

type countsRepo struct {
	service.FilmRepository
	counts film.EntryCounts
	err    error
	userID uint
}

func (r *countsRepo) EntryCounts(_ context.Context, userID uint) (film.EntryCounts, error) {
	r.userID = userID
	return r.counts, r.err
}

func countsEngine(repo *countsRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.ContextWithFallback = true
	signedIn := func(c *gin.Context) {
		principal := auth.Principal{UserID: 7}
		c.Request = c.Request.WithContext(auth.WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
	films := service.NewFilmService(repo, nil, nil, nil)
	NewFilmHandler(slog.New(slog.DiscardHandler), films, nil, nil).Register(engine.Group("/api/v1"), signedIn, signedIn)
	return engine
}

func TestMyFilmCountsAnswersAllSets(t *testing.T) {
	t.Parallel()

	repo := &countsRepo{counts: film.EntryCounts{Watchlist: 3, Watched: 10, Rated: 6, Watching: 1}}
	recorder := httptest.NewRecorder()
	countsEngine(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/films/my/counts", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if repo.userID != 7 {
		t.Errorf("counted for user %d, want the signed-in 7", repo.userID)
	}
	var body map[string]int
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	want := map[string]int{"watchlist": 3, "watched": 10, "rated": 6, "watching": 1}
	if len(body) != len(want) {
		t.Errorf("body %v, want exactly %v", body, want)
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("%s = %d, want %d", key, body[key], value)
		}
	}
}

func TestMyFilmCountsHidesStorageError(t *testing.T) {
	t.Parallel()

	repo := &countsRepo{err: errors.New("pq: relation film_entries does not exist")}
	recorder := httptest.NewRecorder()
	countsEngine(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/films/my/counts", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body["error"] != internalErrorText {
		t.Errorf("body %s, want the generic error text", recorder.Body)
	}
}
