package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"lifelog-api/internal/model"
	"lifelog-api/internal/service"

	"github.com/gin-gonic/gin"
)

type PersonHandler struct {
	handler
	logger  *slog.Logger
	service *service.PersonService
}

func NewPersonHandler(logger *slog.Logger, personService *service.PersonService) *PersonHandler {
	return &PersonHandler{logger: logger, service: personService}
}

func (ph *PersonHandler) Register(rg *gin.RouterGroup) {
	rg.GET("/people/:id", ph.card)
}

func (ph *PersonHandler) card(ctx *gin.Context) {
	id, err := ph.paramID(ctx)
	if err != nil {
		ph.badRequestJSON(ctx, err)
		return
	}

	card, err := ph.service.Card(ctx, id)
	if err != nil {
		if _, notFound := errors.AsType[*model.EntityNotFoundError](err); notFound {
			ph.notFoundJSON(ctx, err)
			return
		}
		ph.logger.Error("карточка человека не выдана", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.JSON(http.StatusOK, card)
}
