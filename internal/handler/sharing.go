package handler

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"lifelog-api/internal/film"
	"lifelog-api/internal/model"
	"lifelog-api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type SharingHandler struct {
	handler
	logger  *slog.Logger
	service *service.SharingService
	pages   *template.Template
}

func NewSharingHandler(logger *slog.Logger, sharing *service.SharingService) *SharingHandler {
	return &SharingHandler{logger: logger, service: sharing, pages: mustParsePages()}
}

func (h *SharingHandler) Register(rg *gin.RouterGroup, requireAuth gin.HandlerFunc) {
	rg.GET("/me/visibility", requireAuth, h.visibility)
	rg.PUT("/me/visibility", requireAuth, h.setVisibility)
	rg.GET("/users/:id/films", requireAuth, h.userFilms)
	rg.POST("/me/share-links", requireAuth, h.createShareLink)
	rg.GET("/me/share-links", requireAuth, h.shareLinks)
	rg.DELETE("/me/share-links/:token", requireAuth, h.revokeShareLink)
	rg.GET("/shared/:token", h.sharedList)
}

type visibilityResponse struct {
	Watchlist string `json:"watchlist"`
	Watched   string `json:"watched"`
	Rated     string `json:"rated"`
}

type visibilityRequest struct {
	Watchlist *string `json:"watchlist"`
	Watched   *string `json:"watched"`
	Rated     *string `json:"rated"`
}

func (h *SharingHandler) visibility(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}

	current, err := h.service.Visibility(ctx, actor.UserID)
	if err != nil {
		h.logger.Error("видимость списков не выдана", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusOK, visibilityResponse{
		Watchlist: current.Watchlist,
		Watched:   current.Watched,
		Rated:     current.Rated,
	})
}

func (h *SharingHandler) setVisibility(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}

	var request visibilityRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}

	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	err := h.service.SetVisibility(writeCtx, actor.UserID, request.Watchlist, request.Watched, request.Rated)
	if errors.Is(err, service.ErrBadVisibility) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.logger.Error("видимость списков не сохранена", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (h *SharingHandler) userFilms(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}

	ownerID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		badRequest(ctx, badIDText)
		return
	}

	limit, _ := queryInt(ctx, "limit")

	page, err := h.service.FilmsOf(ctx, actor.AuthUserID, ownerID, actor.UserID, service.MyFilmsRequest{
		Set:    ctx.Query("set"),
		Sort:   ctx.Query("sort"),
		Limit:  limit,
		Cursor: ctx.Query("cursor"),
	})
	switch {
	case err == nil:
		ctx.JSON(http.StatusOK, page)
	case errors.Is(err, service.ErrListHidden):
		ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, film.ErrUnknownEntrySet), errors.Is(err, film.ErrSortNotApplicable):
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
			h.notFoundJSON(ctx, err)
			return
		}
		h.logger.Error("чужой список не выдан", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

type shareLinkRequest struct {
	Set    string `json:"set"`
	FilmID *uint  `json:"film_id"`

	ExpiresInMinutes int `json:"expires_in_minutes"`
}

func (h *SharingHandler) createShareLink(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}

	var request shareLinkRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}

	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	view, err := h.service.CreateShareLink(writeCtx, actor.UserID, request.Set, request.FilmID, request.ExpiresInMinutes)
	if errors.Is(err, service.ErrShareTargetMissing) || errors.Is(err, service.ErrShareTTLOutOfRange) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.logger.Error("ссылка не создана", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusCreated, view)
}

func (h *SharingHandler) shareLinks(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}

	views, err := h.service.ShareLinks(ctx, actor.UserID)
	if err != nil {
		h.logger.Error("ссылки не выданы", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": views})
}

func (h *SharingHandler) revokeShareLink(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}

	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	err := h.service.RevokeShareLink(writeCtx, actor.UserID, ctx.Param("token"))
	if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
		h.notFoundJSON(ctx, err)
		return
	}
	if err != nil {
		h.logger.Error("ссылка не отозвана", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (h *SharingHandler) sharedList(ctx *gin.Context) {
	limit, _ := queryInt(ctx, "limit")

	list, err := h.service.SharedList(ctx, ctx.Param("token"), limit, ctx.Query("cursor"))
	if err != nil {
		if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
			h.notFoundJSON(ctx, err)
			return
		}
		h.logger.Error("список по ссылке не выдан", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}

	ctx.Header("X-Robots-Tag", "noindex, nofollow")
	ctx.JSON(http.StatusOK, list)
}
