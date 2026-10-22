package handler

import (
	"errors"
	"lifelog-api/internal/auth"
	"lifelog-api/internal/model"
	"lifelog-api/internal/place"
	"lifelog-api/internal/service"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

type PlaceHandler struct {
	handler
	logger  *slog.Logger
	service *service.PlaceService
}

func NewPlaceHandler(logger *slog.Logger, service *service.PlaceService) *PlaceHandler {
	return &PlaceHandler{logger: logger, service: service}
}

func (ph *PlaceHandler) Register(rg *gin.RouterGroup, requireAuth, optionalAuth gin.HandlerFunc) {
	placeGroup := rg.Group("/places")
	placeGroup.POST("", requireAuth, ph.create)
	placeGroup.GET("/:id", optionalAuth, ph.get)
	placeGroup.PUT("/:id", requireAuth, ph.update)
	placeGroup.DELETE("/:id", requireAuth, ph.delete)
	placeGroup.GET("", optionalAuth, ph.getList)
	placeGroup.POST("/:id/review", requireAuth, ph.addReview)
}

func viewerOf(ctx *gin.Context) place.Viewer {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return place.Viewer{}
	}
	return place.Viewer{UserID: principal.UserID, Admin: principal.Role == auth.RoleAdmin}
}

func (ph *PlaceHandler) fail(ctx *gin.Context, logMsg string, err error) {
	switch {
	case errors.Is(err, service.ErrPlaceNotYours):
		ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrUnknownKind),
		errors.Is(err, service.ErrReviewTooLong),
		errors.Is(err, place.ErrUnknownGeography):
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
			ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		internalError(ctx, ph.logger, logMsg, err)
	}
}

func (ph *PlaceHandler) create(ctx *gin.Context) {
	placeRequest := service.CreatePlaceRequest{}
	if errBindJSON := ctx.ShouldBindJSON(&placeRequest); errBindJSON != nil {
		badRequest(ctx, bindErrorMessage(errBindJSON, &placeRequest))
		return
	}
	actor, ok := requirePrincipal(ctx, ph.logger)
	if !ok {
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	req, err := ph.service.Create(writeCtx, actor.UserID, &placeRequest)
	if err != nil {
		ph.fail(ctx, "место не создано", err)
		return
	}
	ctx.JSON(http.StatusCreated, service.CreatePlaceResponse{Id: req.ID})
}

func (ph *PlaceHandler) update(ctx *gin.Context) {
	id, err := ph.paramID(ctx)
	if err != nil {
		ph.badRequestJSON(ctx, err)
		return
	}
	placeRequest := service.UpdatePlaceRequest{}
	if errBindJSON := ctx.ShouldBindJSON(&placeRequest); errBindJSON != nil {
		badRequest(ctx, bindErrorMessage(errBindJSON, &placeRequest))
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := ph.service.Update(writeCtx, id, viewerOf(ctx), &placeRequest); err != nil {
		ph.fail(ctx, "место не изменено", err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (ph *PlaceHandler) get(ctx *gin.Context) {
	id, err := ph.paramID(ctx)
	if err != nil {
		ph.badRequestJSON(ctx, err)
		return
	}
	resp, err := ph.service.FindById(ctx, id, viewerOf(ctx))
	if err != nil {
		ph.fail(ctx, "место не выдано", err)
		return
	}
	ctx.JSON(http.StatusOK, resp)
}

func (ph *PlaceHandler) getList(ctx *gin.Context) {
	limit, ok := queryInt(ctx, "limit")
	if !ok {
		return
	}
	cityID, ok := queryInt(ctx, "city_id")
	if !ok {
		return
	}

	kind := ctx.Query("kind")
	if kind != "" && !place.Kind(kind).Valid() {
		ph.badRequestJSON(ctx, service.ErrUnknownKind)
		return
	}

	page, err := ph.service.FindAll(ctx, service.PlaceListRequest{
		CountryCode: ctx.Query("country"),
		CityID:      uint(cityID),
		Kind:        kind,
		Query:       ctx.Query("q"),
		Cursor:      ctx.Query("cursor"),
		Limit:       limit,
		Viewer:      viewerOf(ctx),
	})
	if err != nil {
		ph.fail(ctx, "список мест не выдан", err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}

func (ph *PlaceHandler) delete(ctx *gin.Context) {
	id, err := ph.paramID(ctx)
	if err != nil {
		ph.badRequestJSON(ctx, err)
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	if err := ph.service.Delete(writeCtx, id, viewerOf(ctx)); err != nil {
		ph.fail(ctx, "место не удалено", err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (ph *PlaceHandler) addReview(ctx *gin.Context) {
	id, err := ph.paramID(ctx)
	if err != nil {
		ph.badRequestJSON(ctx, err)
		return
	}
	revRequest := service.AddReviewRequest{}
	if errBindJSON := ctx.ShouldBindJSON(&revRequest); errBindJSON != nil {
		badRequest(ctx, bindErrorMessage(errBindJSON, &revRequest))
		return
	}
	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	resp, err := ph.service.AddReview(writeCtx, id, viewerOf(ctx), &revRequest)
	if err != nil {
		ph.fail(ctx, "отзыв о месте не сохранён", err)
		return
	}
	ctx.JSON(http.StatusCreated, service.CreatePlaceResponse{Id: resp.ID})
}
