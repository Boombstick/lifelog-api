package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestРегистрацияМаршрутовФильмов(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	passthrough := func(ctx *gin.Context) { ctx.Next() }

	NewFilmHandler(nil, nil, nil, nil).Register(engine.Group("/api/v1"), passthrough, passthrough)

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, want := range []string{
		"GET /api/v1/films",
		"GET /api/v1/films/genres",
		"GET /api/v1/films/my",
		"GET /api/v1/films/my/counts",
		"GET /api/v1/films/:id",
		"GET /api/v1/films/:id/entry",
		"POST /api/v1/films/:id/watchlist",
		"POST /api/v1/films/:id/watched",
		"POST /api/v1/films/:id/review",
		"POST /api/v1/films/:id/seasons/watched",
		"PUT /api/v1/films/:id/seasons/:number",
		"POST /api/v1/films/:id/dropped",
		"DELETE /api/v1/films/:id/dropped",
	} {
		if !registered[want] {
			t.Errorf("маршрут %q не зарегистрирован", want)
		}
	}
}
