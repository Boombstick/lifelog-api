package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"lifelog-api/internal/service"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	handler
	logger *slog.Logger
	sync   *service.FilmSyncService
}

func NewAdminHandler(logger *slog.Logger, sync *service.FilmSyncService) *AdminHandler {
	return &AdminHandler{logger: logger, sync: sync}
}

func (ah *AdminHandler) Register(rg *gin.RouterGroup, requireAuth, requireAdmin gin.HandlerFunc) {
	adminGroup := rg.Group("/admin", requireAuth, requireAdmin)
	adminGroup.POST("/films/sync", ah.startSync)
	adminGroup.GET("/films/sync", ah.syncStatus)
}

func (ah *AdminHandler) startSync(ctx *gin.Context) {
	req := service.StartSyncRequest{}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		badRequest(ctx, bindErrorMessage(err, &req))
		return
	}

	status, err := ah.sync.Start(req)
	switch {
	case err == nil:
		ah.logger.Info("синхронизация каталога запущена",
			"run_id", status.RunID, "year_from", status.YearFrom, "year_to", status.YearTo)
		ctx.JSON(http.StatusAccepted, status)
	case errors.Is(err, service.ErrSyncYears):
		ah.badRequestJSON(ctx, err)
	case errors.Is(err, service.ErrSyncAlreadyRunning):
		ctx.JSON(http.StatusConflict, status)
	case errors.Is(err, service.ErrSyncUnavailable):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		ah.logger.Error("запуск синхронизации не удался", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (ah *AdminHandler) syncStatus(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, ah.sync.Status())
}
