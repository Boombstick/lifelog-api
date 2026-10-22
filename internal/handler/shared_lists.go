package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"lifelog-api/internal/model"
	"lifelog-api/internal/service"
	"lifelog-api/internal/sharedlist"

	"github.com/gin-gonic/gin"
)

type SharedListHandler struct {
	handler
	logger  *slog.Logger
	service *service.SharedListService
}

func NewSharedListHandler(logger *slog.Logger, lists *service.SharedListService) *SharedListHandler {
	return &SharedListHandler{logger: logger, service: lists}
}

func (h *SharedListHandler) Register(rg *gin.RouterGroup, requireAuth gin.HandlerFunc) {
	group := rg.Group("/shared-lists", requireAuth)
	group.GET("", h.lists)
	group.POST("", h.create)
	group.GET("/:id", h.get)
	group.PATCH("/:id", h.update)
	group.DELETE("/:id", h.delete)
	group.POST("/:id/leave", h.leave)
	group.POST("/:id/items", h.addItem)
	group.DELETE("/:id/items/:film_id", h.removeItem)
	group.POST("/:id/draws", h.draw)
	group.GET("/:id/draws", h.draws)
}

type sharedListCreateRequest struct {
	Title     string   `json:"title"`
	MemberIDs []string `json:"member_ids"`
}

type sharedListUpdateRequest struct {
	Title     *string  `json:"title"`
	MemberIDs []string `json:"member_ids"`
}

type sharedListItemRequest struct {
	FilmID uint `json:"film_id"`
}

type sharedListDrawRequest struct {
	Mode  string `json:"mode"`
	Count *int   `json:"count"`
}

func (h *SharedListHandler) fail(ctx *gin.Context, logMsg string, err error) {
	if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
		h.notFoundJSON(ctx, err)
		return
	}
	if _, ok := errors.AsType[*service.NotSubscribersError](err); ok {
		h.badRequestJSON(ctx, err)
		return
	}
	if _, ok := errors.AsType[*service.BadMemberIDError](err); ok {
		h.badRequestJSON(ctx, err)
		return
	}
	switch {
	case errors.Is(err, service.ErrSharedListTitle),
		errors.Is(err, service.ErrSharedListSelfMember),
		errors.Is(err, service.ErrSharedListTooMany),
		errors.Is(err, service.ErrSharedListOwnerLeave),
		errors.Is(err, service.ErrSharedListBadMode),
		errors.Is(err, service.ErrSharedListBadCount),
		errors.Is(err, service.ErrSharedListBadFilm):
		h.badRequestJSON(ctx, err)
	case errors.Is(err, service.ErrSharedListNotOwner):
		h.forbiddenRequestJSON(ctx, err)
	case errors.Is(err, sharedlist.ErrListFull), errors.Is(err, sharedlist.ErrListEmpty):
		ctx.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrFollowsUnavailable):
		h.logger.Warn(logMsg, "error", err)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": service.ErrFollowsUnavailable.Error()})
	case errors.Is(err, service.ErrBlocksUnavailable):
		h.logger.Warn(logMsg, "error", err)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": service.ErrBlocksUnavailable.Error()})
	default:
		h.logger.Error(logMsg, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (h *SharedListHandler) listID(ctx *gin.Context) (uint, bool) {
	id, err := h.paramID(ctx)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": model.NewEntityNotFound("shared list").Error()})
		return 0, false
	}
	return id, true
}

func (h *SharedListHandler) lists(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	page, err := h.service.Lists(ctx, actor.AuthUserID)
	if err != nil {
		h.fail(ctx, "совместные списки не выданы", err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}

func (h *SharedListHandler) create(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	var request sharedListCreateRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	list, err := h.service.Create(writeCtx, actor.AuthUserID, request.Title, request.MemberIDs)
	if err != nil {
		h.fail(ctx, "совместный список не создан", err)
		return
	}
	ctx.JSON(http.StatusCreated, list)
}

func (h *SharedListHandler) get(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	list, err := h.service.Get(ctx, id, actor.AuthUserID)
	if err != nil {
		h.fail(ctx, "совместный список не выдан", err)
		return
	}
	ctx.JSON(http.StatusOK, list)
}

func (h *SharedListHandler) update(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	var request sharedListUpdateRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	list, err := h.service.Update(writeCtx, id, actor.AuthUserID, request.Title, request.MemberIDs)
	if err != nil {
		h.fail(ctx, "совместный список не изменён", err)
		return
	}
	ctx.JSON(http.StatusOK, list)
}

func (h *SharedListHandler) delete(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := h.service.Delete(writeCtx, id, actor.AuthUserID); err != nil {
		h.fail(ctx, "совместный список не удалён", err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (h *SharedListHandler) leave(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := h.service.Leave(writeCtx, id, actor.AuthUserID); err != nil {
		h.fail(ctx, "выход из списка не выполнен", err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (h *SharedListHandler) addItem(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	var request sharedListItemRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	item, created, err := h.service.AddItem(writeCtx, id, actor.AuthUserID, request.FilmID)
	if err != nil {
		h.fail(ctx, "фильм не добавлен в совместный список", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ctx.JSON(status, item)
}

func (h *SharedListHandler) removeItem(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	filmID, err := h.param(ctx, "film_id")
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": model.NewEntityNotFound("shared list item").Error()})
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := h.service.RemoveItem(writeCtx, id, actor.AuthUserID, filmID); err != nil {
		h.fail(ctx, "фильм не убран из совместного списка", err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (h *SharedListHandler) draw(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	var request sharedListDrawRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	draw, err := h.service.Draw(writeCtx, id, actor.AuthUserID, request.Mode, request.Count)
	if err != nil {
		h.fail(ctx, "розыгрыш не выполнен", err)
		return
	}
	ctx.JSON(http.StatusCreated, draw)
}

func (h *SharedListHandler) draws(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, ok := h.listID(ctx)
	if !ok {
		return
	}
	limit, _ := queryInt(ctx, "limit")
	page, err := h.service.Draws(ctx, id, actor.AuthUserID, limit)
	if err != nil {
		h.fail(ctx, "история розыгрышей не выдана", err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}
