package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdbadd"

	"github.com/gin-gonic/gin"
)

type TMDBAdder interface {
	Search(ctx context.Context, rawQuery string, page int) (tmdbadd.SearchResponse, error)
	Add(ctx context.Context, userID uint, kind film.Kind, tmdbID int) (tmdbadd.AddResponse, error)
}

const addWriteDeadline = tmdbadd.AddBudget + 10*time.Second

type TMDBAddHandler struct {
	handler
	logger  *slog.Logger
	service TMDBAdder
}

func NewTMDBAddHandler(logger *slog.Logger, service TMDBAdder) *TMDBAddHandler {
	return &TMDBAddHandler{logger: logger, service: service}
}

func (h *TMDBAddHandler) Register(rg *gin.RouterGroup, requireAuth gin.HandlerFunc) {
	group := rg.Group("/tmdb", requireAuth)
	group.GET("/search", h.search)
	group.POST("/add", h.add)
}

func (h *TMDBAddHandler) fail(ctx *gin.Context, logMsg string, err error) {
	switch {
	case errors.Is(err, tmdbadd.ErrQuery), errors.Is(err, tmdbadd.ErrPage),
		errors.Is(err, tmdbadd.ErrKind), errors.Is(err, tmdbadd.ErrTMDBID),
		errors.Is(err, tmdbadd.ErrAdult):
		h.badRequestJSON(ctx, err)
	case errors.Is(err, tmdbadd.ErrNotFound):
		h.notFoundJSON(ctx, err)
	case errors.Is(err, tmdbadd.ErrLimit):
		ctx.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
	case errors.Is(err, tmdbadd.ErrUnavailable):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case ctx.Request.Context().Err() != nil:
		ctx.AbortWithStatus(statusClientClosed)
	default:
		h.logger.Error(logMsg, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (h *TMDBAddHandler) search(ctx *gin.Context) {
	page := 1
	if raw, ok := ctx.GetQuery("page"); ok {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.badRequestJSON(ctx, tmdbadd.ErrPage)
			return
		}
		page = parsed
	}
	resp, err := h.service.Search(ctx, ctx.Query("query"), page)
	if err != nil {
		h.fail(ctx, "tmdb search failed", err)
		return
	}
	ctx.JSON(http.StatusOK, resp)
}

type tmdbAddRequest struct {
	Kind string `json:"kind"`

	TMDBID *int64 `json:"tmdb_id"`
}

func (h *TMDBAddHandler) add(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	var req tmdbAddRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		badRequest(ctx, malformedRequest)
		return
	}
	kind, err := tmdbadd.ParseKind(req.Kind)
	if err != nil {
		h.badRequestJSON(ctx, err)
		return
	}
	if req.TMDBID == nil || *req.TMDBID <= 0 || *req.TMDBID > 1<<31-1 {
		h.badRequestJSON(ctx, tmdbadd.ErrTMDBID)
		return
	}

	if err := http.NewResponseController(ctx.Writer).SetWriteDeadline(time.Now().Add(addWriteDeadline)); err != nil {
		h.logger.Warn("tmdb add: write deadline not extended", "error", err)
	}

	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	resp, err := h.service.Add(writeCtx, actor.UserID, kind, int(*req.TMDBID))
	if err != nil {
		h.fail(ctx, "tmdb add failed", err)
		return
	}
	status := http.StatusOK
	if resp.Created {
		status = http.StatusCreated
	}
	ctx.JSON(status, resp)
}
