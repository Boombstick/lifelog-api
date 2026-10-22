package handler

import (
	"errors"
	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"
	"lifelog-api/internal/service"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type SearchHandler struct {
	logger  *slog.Logger
	service *service.SearchService
}

func NewSearchHandler(logger *slog.Logger, searchService *service.SearchService) *SearchHandler {
	return &SearchHandler{logger: logger, service: searchService}
}

func (sh *SearchHandler) Register(rg *gin.RouterGroup, optionalAuth gin.HandlerFunc) {
	searchGroup := rg.Group("/search", optionalAuth)
	searchGroup.GET("", sh.combined)
	searchGroup.GET("/films", sh.films)
	searchGroup.GET("/people", sh.people)
}

func (sh *SearchHandler) combined(ctx *gin.Context) {
	req, ok := parseSearchRequest(ctx, true)
	if !ok {
		return
	}

	result, err := sh.service.Combined(ctx, req)
	if err != nil {
		sh.searchFailed(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func (sh *SearchHandler) films(ctx *gin.Context) {
	req, ok := parseSearchRequest(ctx, false)
	if !ok {
		return
	}

	page, err := sh.service.Films(ctx, req)
	if err != nil {
		sh.searchFailed(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}

func (sh *SearchHandler) people(ctx *gin.Context) {
	req, ok := parseSearchRequest(ctx, true)
	if !ok {
		return
	}

	page, err := sh.service.People(ctx, req)
	if err != nil {
		sh.searchFailed(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}

func parseSearchRequest(ctx *gin.Context, requireText bool) (service.SearchRequest, bool) {
	text := strings.TrimSpace(ctx.Query("q"))
	if requireText && text == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "параметр q обязателен"})
		return service.SearchRequest{}, false
	}

	limit, ok := queryInt(ctx, "limit")
	if !ok {
		return service.SearchRequest{}, false
	}
	filters, ok := parseSearchFilters(ctx)
	if !ok {
		return service.SearchRequest{}, false
	}

	req := service.SearchRequest{
		Text:        text,
		Filters:     filters,
		Scope:       ctx.Query("scope"),
		Sort:        ctx.Query("sort"),
		Order:       ctx.Query("order"),
		HideWatched: ctx.Query("hide_watched") == "true",
		Limit:       limit,
		Cursor:      ctx.Query("cursor"),
		WithFacets:  ctx.Query("facets") == "true",
	}

	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		req.UserID = principal.UserID
	}

	if err := req.Validate(); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, service.ErrAnonymousHide) {
			status = http.StatusUnauthorized
		}
		ctx.JSON(status, gin.H{"error": err.Error()})
		return service.SearchRequest{}, false
	}
	return req, true
}

func parseSearchFilters(ctx *gin.Context) (service.FilmFilters, bool) {
	var filters service.FilmFilters

	switch kind := strings.TrimSpace(ctx.Query("kind")); kind {
	case "", string(film.KindMovie), string(film.KindSeries):
		filters.Kind = kind
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "kind должен быть movie или series"})
		return service.FilmFilters{}, false
	}

	genres := ctx.QueryArray("genre")
	if len(genres) == 1 {
		genres = strings.Split(genres[0], ",")
	}
	for _, genre := range genres {
		genre = strings.TrimSpace(genre)
		if genre != "" {
			filters.Genres = append(filters.Genres, genre)
		}
	}

	for _, field := range []struct {
		name string
		dst  *int
	}{
		{"year_from", &filters.YearFrom},
		{"year_to", &filters.YearTo},
		{"runtime_from", &filters.RuntimeFrom},
		{"runtime_to", &filters.RuntimeTo},
	} {
		value, ok := queryInt(ctx, field.name)
		if !ok {
			return service.FilmFilters{}, false
		}
		*field.dst = value
	}

	for _, field := range []struct {
		name string
		dst  *float64
	}{
		{"rating_from", &filters.RatingFrom},
		{"local_from", &filters.LocalFrom},
	} {
		value, ok := queryFloat(ctx, field.name)
		if !ok {
			return service.FilmFilters{}, false
		}
		if value < 0 || value > 10 {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": field.name + " должен быть от 0 до 10"})
			return service.FilmFilters{}, false
		}
		*field.dst = value
	}

	return filters, true
}

func queryInt(ctx *gin.Context, name string) (int, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": name + " должен быть целым числом"})
		return 0, false
	}
	return value, true
}

func queryFloat(ctx *gin.Context, name string) (float64, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseFloat(raw, 64)

	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": name + " должен быть числом"})
		return 0, false
	}
	return value, true
}

func (sh *SearchHandler) searchFailed(ctx *gin.Context, err error) {
	searchUnavailable(ctx, sh.logger, "поиск не выполнен", "поиск временно недоступен", err)
}
