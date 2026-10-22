package handler

import (
	"context"
	"errors"
	"lifelog-api/internal/auth"
	"lifelog-api/internal/middleware"
	"lifelog-api/internal/service"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const statusClientClosed = 499

const (
	cachePublicReference  = "public, max-age=3600"
	cachePrivateReference = "private, max-age=3600"
)

func searchUnavailable(ctx *gin.Context, logger *slog.Logger, logMsg, publicMsg string, err error) {
	switch {
	case ctx.Request.Context().Err() != nil:
		ctx.AbortWithStatus(statusClientClosed)
	case errors.Is(err, service.ErrSearchOverloaded), errors.Is(err, service.ErrSearchTimeout):
		logger.Warn(logMsg, "error", err)
		ctx.Header("Retry-After", "1")
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": publicMsg})
	default:
		logger.Error(logMsg, "error", err)
		ctx.JSON(http.StatusBadGateway, gin.H{"error": publicMsg})
	}
}

type handler struct {
}

func (h handler) badRequestJSON(ctx *gin.Context, err error) {
	ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func (h handler) notFoundJSON(ctx *gin.Context, err error) {
	ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
}

func (h handler) forbiddenRequestJSON(ctx *gin.Context, err error) {
	ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
}

func requirePrincipal(ctx *gin.Context, logger *slog.Logger) (auth.Principal, bool) {
	principal, err := auth.RequirePrincipal(ctx)
	if err != nil {
		internalError(ctx, logger, "missing principal on protected route", err)
		return auth.Principal{}, false
	}
	return principal, true
}

func requireAccount(c *gin.Context, logger *slog.Logger) (uuid.UUID, bool) {
	id, ok := middleware.AccountID(c)
	if !ok {
		internalError(c, logger, "missing account on protected route", errMissingAccount)
		return uuid.Nil, false
	}
	return id, true
}

var errMissingAccount = errors.New("no account id in request context")

func (h handler) paramID(ctx *gin.Context) (uint, error) {
	return h.param(ctx, "id")
}

func (h handler) param(ctx *gin.Context, name string) (uint, error) {
	id, err := strconv.ParseUint(ctx.Param(name), 10, 63)
	if err != nil || id == 0 {
		return 0, errBadID
	}
	return uint(id), nil
}

const badIDText = "Некорректный идентификатор"

var errBadID = errors.New(badIDText)

const internalErrorText = "Внутренняя ошибка сервера"

func internalError(ctx *gin.Context, logger *slog.Logger, logMsg string, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.ErrorContext(ctx, logMsg, "error", err)
	ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
}

const writeTimeout = 30 * time.Second

func writeContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(c.Request.Context()), writeTimeout)
}
