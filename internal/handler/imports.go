package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"lifelog-api/internal/imdbimport"

	"github.com/gin-gonic/gin"
)

const (
	maxImportFile    = 5 << 20
	maxImportRequest = maxImportFile + 64<<10
)

var errImportTooLarge = errors.New("файл больше 5 МБ")

type ImportHandler struct {
	handler
	logger  *slog.Logger
	service *imdbimport.Service
}

func NewImportHandler(logger *slog.Logger, service *imdbimport.Service) *ImportHandler {
	return &ImportHandler{logger: logger, service: service}
}

func (h *ImportHandler) Register(rg *gin.RouterGroup, requireAuth gin.HandlerFunc) {
	group := rg.Group("/imports", requireAuth)
	group.POST("/imdb", h.startIMDb)
	group.GET("", h.list)
	group.GET("/:id", h.get)
}

func (h *ImportHandler) fail(ctx *gin.Context, logMsg string, err error) {
	switch {
	case errors.Is(err, imdbimport.ErrNotIMDb), errors.Is(err, imdbimport.ErrTooManyRows):
		h.badRequestJSON(ctx, err)
	case errors.Is(err, imdbimport.ErrImportRunning):
		ctx.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, imdbimport.ErrJobNotFound):
		h.notFoundJSON(ctx, err)
	case errors.Is(err, imdbimport.ErrUnavailable), errors.Is(err, imdbimport.ErrShuttingDown):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		h.logger.Error(logMsg, "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (h *ImportHandler) startIMDb(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}

	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxImportRequest)
	header, err := ctx.FormFile("file")
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			ctx.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": errImportTooLarge.Error()})
			return
		}
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужен файл в поле file"})
		return
	}
	if header.Size > maxImportFile {
		ctx.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": errImportTooLarge.Error()})
		return
	}
	file, err := header.Open()
	if err != nil {
		h.fail(ctx, "import file not opened", err)
		return
	}
	defer file.Close()

	writeCtx, cancel := writeContext(ctx)
	defer cancel()
	job, err := h.service.Start(writeCtx, actor.UserID, file)
	if err != nil {
		h.fail(ctx, "imdb import not started", err)
		return
	}
	ctx.JSON(http.StatusAccepted, job)
}

func (h *ImportHandler) list(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	jobs, err := h.service.List(ctx, actor.UserID)
	if err != nil {
		h.fail(ctx, "imports not listed", err)
		return
	}
	ctx.JSON(http.StatusOK, jobs)
}

func (h *ImportHandler) get(ctx *gin.Context) {
	actor, ok := requirePrincipal(ctx, h.logger)
	if !ok {
		return
	}
	id, err := h.paramID(ctx)
	if err != nil {
		h.notFoundJSON(ctx, imdbimport.ErrJobNotFound)
		return
	}
	job, err := h.service.Get(ctx, actor.UserID, id)
	if err != nil {
		h.fail(ctx, "import not read", err)
		return
	}
	ctx.JSON(http.StatusOK, job)
}
