package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"lifelog-api/internal/film"
	"lifelog-api/internal/model"

	"github.com/gin-gonic/gin"
)

func (fh *FilmHandler) markError(ctx *gin.Context, err error, logMessage string) {
	switch {
	case errors.Is(err, film.ErrNotSeries), errors.Is(err, film.ErrSeriesDerived),
		errors.Is(err, film.ErrSeriesReviewNeedsSeason), errors.Is(err, film.ErrBadSeasonState),
		errors.Is(err, film.ErrSeasonRate):
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, film.ErrSeasonNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, film.ErrSeasonNotAired):
		ctx.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, film.ErrRatedCannotUnwatch):
		ctx.JSON(http.StatusConflict, gin.H{"error": film.ErrRatedCannotUnwatch.Error()})
	default:
		if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
			fh.notFoundJSON(ctx, err)
			return
		}
		fh.logger.Error(logMessage, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (fh *FilmHandler) seriesAction(ctx *gin.Context, action func(c context.Context, filmID, userID uint) error) {
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
	if err := action(writeCtx, id, actor.UserID); err != nil {
		fh.markError(ctx, err, "series tracking failed")
		return
	}
	ctx.Status(http.StatusNoContent)
}

type seasonStateRequest struct {
	State json.RawMessage `json:"state" swaggertype:"string"`
	Rate  json.RawMessage `json:"rate" swaggertype:"integer"`
}

type seasonUpdate struct {
	state *string

	rateOnly bool
	rate     *int16
	setRate  bool
}

func parseSeasonUpdate(request seasonStateRequest) (seasonUpdate, error) {
	var update seasonUpdate
	update.setRate = len(request.Rate) > 0
	if update.setRate && string(request.Rate) != "null" {
		var value int
		if err := json.Unmarshal(request.Rate, &value); err != nil || value < 1 || value > 10 {
			return update, film.ErrSeasonRate
		}
		converted := int16(value)
		update.rate = &converted
	}

	if len(request.State) == 0 {
		update.rateOnly = update.setRate
		return update, nil
	}
	if string(request.State) == "null" {
		return update, nil
	}
	var state string
	if err := json.Unmarshal(request.State, &state); err != nil {
		return update, film.ErrBadSeasonState
	}
	update.state = &state
	return update, nil
}

func (fh *FilmHandler) setSeason(ctx *gin.Context) {
	number, err := strconv.Atoi(ctx.Param("number"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "номер сезона должен быть числом"})
		return
	}
	var request seasonStateRequest
	if err := ctx.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}

	update, err := parseSeasonUpdate(request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fh.seriesAction(ctx, func(c context.Context, filmID, userID uint) error {
		if update.rateOnly {
			return fh.service.SetSeasonRate(c, filmID, userID, number, update.rate)
		}
		return fh.service.SetSeason(c, filmID, userID, number, update.state, update.rate, update.setRate)
	})
}

type markSeasonsRequest struct {
	UpTo *int `json:"up_to"`
}

func (fh *FilmHandler) markAiredSeasons(ctx *gin.Context) {
	var request markSeasonsRequest
	if err := ctx.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		badRequest(ctx, bindErrorMessage(err, &request))
		return
	}
	fh.seriesAction(ctx, func(c context.Context, filmID, userID uint) error {
		return fh.service.MarkAiredSeasons(c, filmID, userID, request.UpTo)
	})
}

func (fh *FilmHandler) setDropped(ctx *gin.Context) {
	fh.seriesAction(ctx, fh.service.SetDropped)
}

func (fh *FilmHandler) clearDropped(ctx *gin.Context) {
	fh.seriesAction(ctx, fh.service.ClearDropped)
}
