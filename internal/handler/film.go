package handler

import (
	"context"
	"errors"
	"strings"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"
	"lifelog-api/internal/model"
	"lifelog-api/internal/service"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

type FilmHandler struct {
	handler
	logger  *slog.Logger
	service *service.FilmService

	catalog   *service.SearchService
	recommend *service.RecommendService
}

func NewFilmHandler(logger *slog.Logger, service *service.FilmService, catalog *service.SearchService, recommend *service.RecommendService) *FilmHandler {
	return &FilmHandler{logger: logger, service: service, catalog: catalog, recommend: recommend}
}

func (fh *FilmHandler) Register(rg *gin.RouterGroup, requireAuth, optionalAuth gin.HandlerFunc) {
	filmGroup := rg.Group("/films")

	filmGroup.GET("", optionalAuth, fh.getList)

	filmGroup.GET("/genres", fh.genres)
	filmGroup.GET("/filters", fh.filterBounds)
	filmGroup.GET("/recommendations", requireAuth, fh.recommendations)
	filmGroup.GET("/my", requireAuth, fh.myFilms)
	filmGroup.GET("/my/counts", requireAuth, fh.myFilmCounts)
	filmGroup.GET("/:id", optionalAuth, fh.get)
	filmGroup.GET("/:id/entry", requireAuth, fh.myEntry)
	filmGroup.GET("/:id/similar", fh.similar)
	filmGroup.POST("/:id/watchlist", requireAuth, fh.addToWatchlist)
	filmGroup.POST("/:id/watched", requireAuth, fh.markWatched)
	filmGroup.POST("/:id/review", requireAuth, fh.addReview)
	filmGroup.DELETE("/:id/watchlist", requireAuth, fh.removeFromWatchlist)
	filmGroup.DELETE("/:id/watched", requireAuth, fh.removeWatched)
	filmGroup.DELETE("/:id/review", requireAuth, fh.removeReview)
	filmGroup.POST("/:id/seasons/watched", requireAuth, fh.markAiredSeasons)
	filmGroup.PUT("/:id/seasons/:number", requireAuth, fh.setSeason)
	filmGroup.POST("/:id/dropped", requireAuth, fh.setDropped)
	filmGroup.DELETE("/:id/dropped", requireAuth, fh.clearDropped)
}

func (fh *FilmHandler) getList(ctx *gin.Context) {
	req, ok := parseSearchRequest(ctx, false)
	if !ok {
		return
	}

	page, err := fh.catalog.Catalog(ctx, req)
	if err != nil {
		searchUnavailable(ctx, fh.logger, "список фильмов не выдан", "каталог временно недоступен", err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}

func (fh *FilmHandler) get(ctx *gin.Context) {
	id, err := fh.paramID(ctx)
	if err != nil {
		fh.badRequestJSON(ctx, err)
		return
	}

	var viewer *auth.Principal
	if actor, ok := auth.PrincipalFromContext(ctx); ok {
		viewer = &actor
	}
	card, err := fh.service.Card(ctx, id, viewer)
	if err != nil {
		if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
			fh.notFoundJSON(ctx, err)
			return
		}
		fh.logger.Error("find film failed", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusOK, card)
}

func (fh *FilmHandler) addToWatchlist(ctx *gin.Context) {
	id, err := fh.paramID(ctx)
	if err != nil {
		fh.badRequestJSON(ctx, err)
		return
	}
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := fh.service.AddToWatchList(writeCtx, id, actor.UserID); err != nil {
		fh.logger.Error("add to watchlist failed", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (fh *FilmHandler) markWatched(ctx *gin.Context) {
	id, err := fh.paramID(ctx)
	if err != nil {
		fh.badRequestJSON(ctx, err)
		return
	}
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := fh.service.AddToWatched(writeCtx, id, actor.UserID); err != nil {
		fh.markError(ctx, err, "mark watched failed")
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (fh *FilmHandler) addReview(ctx *gin.Context) {
	id, err := fh.paramID(ctx)
	if err != nil {
		fh.badRequestJSON(ctx, err)
		return
	}
	req := service.CreateReviewRequest{}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		badRequest(ctx, bindErrorMessage(err, &req))
		return
	}
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := fh.service.AddRateAndDescription(writeCtx, id, actor.UserID, &req); err != nil {
		fh.markError(ctx, err, "add review failed")
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (fh *FilmHandler) removeFromWatchlist(ctx *gin.Context) {
	fh.removeMark(ctx, fh.service.RemoveFromWatchList, "remove from watchlist failed")
}

func (fh *FilmHandler) removeWatched(ctx *gin.Context) {
	fh.removeMark(ctx, fh.service.RemoveFromWatched, "remove watched failed")
}

func (fh *FilmHandler) removeReview(ctx *gin.Context) {
	fh.removeMark(ctx, fh.service.RemoveReview, "remove review failed")
}

func (fh *FilmHandler) removeMark(
	ctx *gin.Context,
	remove func(context.Context, uint, uint) error,
	logMessage string,
) {
	id, err := fh.paramID(ctx)
	if err != nil {
		fh.badRequestJSON(ctx, err)
		return
	}
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}

	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := remove(writeCtx, id, actor.UserID); err != nil {
		fh.markError(ctx, err, logMessage)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (fh *FilmHandler) genres(ctx *gin.Context) {
	genres, err := fh.service.Genres(ctx)
	if err != nil {
		fh.logger.Error("справочник жанров не выдан", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.Header("Cache-Control", cachePublicReference)
	ctx.JSON(http.StatusOK, genres)
}

func (fh *FilmHandler) filterBounds(ctx *gin.Context) {
	bounds, err := fh.catalog.Bounds(ctx)
	if err != nil {
		searchUnavailable(ctx, fh.logger, "границы каталога не выданы", "каталог временно недоступен", err)
		return
	}
	ctx.Header("Cache-Control", cachePublicReference)
	ctx.JSON(http.StatusOK, bounds)
}

func (fh *FilmHandler) myFilms(ctx *gin.Context) {
	limit, ok := queryInt(ctx, "limit")
	if !ok {
		return
	}
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}

	page, err := fh.service.MyFilms(ctx, actor.UserID, service.MyFilmsRequest{
		Set:    ctx.Query("set"),
		Sort:   ctx.Query("sort"),
		Limit:  limit,
		Cursor: ctx.Query("cursor"),
	})
	if err != nil {
		if errors.Is(err, film.ErrUnknownEntrySet) || errors.Is(err, film.ErrSortNotApplicable) {
			fh.badRequestJSON(ctx, err)
			return
		}
		fh.logger.Error("свои фильмы не выданы", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusOK, page)
}

func (fh *FilmHandler) myFilmCounts(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}

	counts, err := fh.service.MyFilmCounts(ctx, actor.UserID)
	if err != nil {
		fh.logger.Error("personal set counts failed", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusOK, counts)
}

func (fh *FilmHandler) myEntry(ctx *gin.Context) {
	id, err := fh.paramID(ctx)
	if err != nil {
		fh.badRequestJSON(ctx, err)
		return
	}
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}

	entry, err := fh.service.MyEntry(ctx, id, actor.UserID)
	if err != nil {
		if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
			fh.notFoundJSON(ctx, err)
			return
		}
		fh.logger.Error("read film entry failed", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusOK, entry)
}

func (fh *FilmHandler) similar(ctx *gin.Context) {
	id, err := fh.paramID(ctx)
	if err != nil {
		fh.badRequestJSON(ctx, err)
		return
	}
	limit, ok := queryInt(ctx, "limit")
	if !ok {
		return
	}

	result, err := fh.recommend.Similar(ctx, id, ctx.Query("source"), limit)
	if err != nil {
		if errors.Is(err, service.ErrUnknownSource) {
			fh.badRequestJSON(ctx, err)
			return
		}
		searchUnavailable(ctx, fh.logger, "похожие не выданы", "подбор временно недоступен", err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func (fh *FilmHandler) recommendations(ctx *gin.Context) {
	limit, ok := queryInt(ctx, "limit")
	if !ok {
		return
	}
	actor, ok := requirePrincipal(ctx, fh.logger)
	if !ok {
		return
	}

	kind := film.Kind(strings.TrimSpace(ctx.Query("kind")))
	switch kind {
	case "":
		kind = film.KindMovie
	case film.KindMovie, film.KindSeries:
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "kind должен быть movie или series"})
		return
	}

	result, err := fh.recommend.ForUser(ctx, actor.UserID, kind, limit)
	if err != nil {
		searchUnavailable(ctx, fh.logger, "рекомендации не выданы", "подбор временно недоступен", err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}
