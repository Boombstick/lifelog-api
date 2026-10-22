package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"lifelog-api/internal/film"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CalendarSource interface {
	Upcoming(ctx context.Context, from, to time.Time, authUser uuid.UUID) ([]film.UpcomingEvent, error)
}

type FollowsInvalidator interface {
	Invalidate(viewers ...uuid.UUID)
}

type BlockApplier interface {
	ApplyBlock(ctx context.Context, a, b uuid.UUID) (int64, error)
}

type InternalHandler struct {
	logger   *slog.Logger
	follows  FollowsInvalidator
	blocks   BlockApplier
	calendar CalendarSource
}

func NewInternalHandler(logger *slog.Logger, follows FollowsInvalidator, blocks BlockApplier, calendar CalendarSource) *InternalHandler {
	return &InternalHandler{logger: logger, follows: follows, blocks: blocks, calendar: calendar}
}

func (h *InternalHandler) Register(group *gin.RouterGroup, guard gin.HandlerFunc) {
	internal := group.Group("/internal", guard)
	internal.POST("/follows/invalidate", h.InvalidateFollows)
	internal.POST("/blocks/applied", h.BlockApplied)
	internal.GET("/calendar/upcoming", h.Upcoming)
}

type blockAppliedRequest struct {
	A string `json:"a" binding:"required"`
	B string `json:"b" binding:"required"`
}

func (h *InternalHandler) BlockApplied(c *gin.Context) {
	var request blockAppliedRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	a, err := uuid.Parse(request.A)
	if err != nil {
		badRequest(c, "a не uuid")
		return
	}
	b, err := uuid.Parse(request.B)
	if err != nil {
		badRequest(c, "b не uuid")
		return
	}
	if a == b {
		badRequest(c, "a и b совпадают")
		return
	}

	if h.follows != nil {
		h.follows.Invalidate(a, b)
	}
	if h.blocks == nil {
		c.Status(http.StatusNoContent)
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	removed, err := h.blocks.ApplyBlock(ctx, a, b)
	if err != nil {
		h.logger.Error("block not applied to shared lists", "a", a, "b", b, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	if removed > 0 {
		h.logger.Info("block applied to shared lists", "a", a, "b", b, "removed", removed)
	}
	c.Status(http.StatusNoContent)
}

type followsInvalidateRequest struct {
	UserIDs []string `json:"user_ids" binding:"required"`
}

func (h *InternalHandler) InvalidateFollows(c *gin.Context) {
	var request followsInvalidateRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	ids := make([]uuid.UUID, 0, len(request.UserIDs))
	for _, raw := range request.UserIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			badRequest(c, "user_ids содержит не uuid")
			return
		}
		ids = append(ids, id)
	}
	if h.follows != nil {
		h.follows.Invalidate(ids...)
	}
	c.Status(http.StatusNoContent)
}

func (h *InternalHandler) Upcoming(c *gin.Context) {
	if h.calendar == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "calendar source not configured"})
		return
	}
	from, err := time.Parse("2006-01-02", c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from must be a date, YYYY-MM-DD"})
		return
	}
	to, err := time.Parse("2006-01-02", c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to must be a date, YYYY-MM-DD"})
		return
	}
	if !to.After(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to must be later than from"})
		return
	}

	var user uuid.UUID
	if raw := c.Query("user"); raw != "" {
		if user, err = uuid.Parse(raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user must be a UUID"})
			return
		}
	}

	events, err := h.calendar.Upcoming(c, from, to, user)
	if err != nil {
		h.logger.Error("calendar upcoming failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": events})
}
