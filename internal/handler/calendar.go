package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"lifelog-api/internal/calendar"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CalendarHandler struct {
	logger  *slog.Logger
	service *calendar.Service

	google *calendar.GoogleImport
}

func NewCalendarHandler(logger *slog.Logger, service *calendar.Service, google *calendar.GoogleImport) *CalendarHandler {
	return &CalendarHandler{logger: logger, service: service, google: google}
}

func (h *CalendarHandler) Register(engine *gin.Engine, auth gin.HandlerFunc) {
	group := engine.Group("/calendar", auth)
	group.GET("/agenda", h.Agenda)
	group.POST("/events", h.CreateEvent)
	group.GET("/events/:id", h.Event)
	group.PUT("/events/:id", h.UpdateEvent)
	group.DELETE("/events/:id", h.DeleteEvent)
	group.GET("/settings", h.Settings)
	group.PUT("/settings", h.SaveSettings)
	h.registerGoogle(engine, group)
}

type eventRequest struct {
	Title    string     `json:"title" binding:"required"`
	Details  string     `json:"details"`
	Location string     `json:"location"`
	StartsAt time.Time  `json:"starts_at" binding:"required"`
	EndsAt   *time.Time `json:"ends_at"`
	AllDay   bool       `json:"all_day"`

	Timezone string `json:"timezone"`
}

func (r eventRequest) toEvent(userID uuid.UUID) *calendar.Event {
	return &calendar.Event{
		UserID:   userID,
		Title:    r.Title,
		Details:  r.Details,
		Location: r.Location,
		StartsAt: r.StartsAt,
		EndsAt:   r.EndsAt,
		AllDay:   r.AllDay,
		Timezone: strings.TrimSpace(r.Timezone),
	}
}

func (h *CalendarHandler) CreateEvent(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	var request eventRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	event := request.toEvent(userID)
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.CreateEvent(ctx, event); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, newEventDTO(event))
}

func (h *CalendarHandler) UpdateEvent(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": badIDText})
		return
	}
	var request eventRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	event := request.toEvent(userID)
	event.ID = id
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.UpdateEvent(ctx, event); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, newEventDTO(event))
}

func (h *CalendarHandler) DeleteEvent(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": badIDText})
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.DeleteEvent(ctx, userID, id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *CalendarHandler) Event(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": badIDText})
		return
	}
	event, err := h.service.Event(c.Request.Context(), userID, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, newEventDTO(event))
}

func (h *CalendarHandler) Agenda(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}

	now := time.Now().UTC()
	from, to := now.AddDate(0, 0, -1), now.AddDate(0, 0, 14)
	if raw := c.Query("from"); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Начало периода — дата в формате ГГГГ-ММ-ДД"})
			return
		}
		from = parsed
	}
	if raw := c.Query("to"); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Конец периода — дата в формате ГГГГ-ММ-ДД"})
			return
		}
		to = parsed
	}
	if !to.After(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Конец периода должен быть позже начала"})
		return
	}

	if to.Sub(from) > 366*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Период не может быть длиннее года"})
		return
	}

	entries, err := h.service.Agenda(c.Request.Context(), userID, from, to)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": entries})
}

func (h *CalendarHandler) Settings(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	settings, err := h.service.SettingsOf(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, newSettingsDTO(settings))
}

type settingsRequest struct {
	DigestAt    string `json:"digest_at" binding:"required"`
	Timezone    string `json:"timezone" binding:"required"`
	Enabled     *bool  `json:"enabled"`
	LeadMinutes *int   `json:"lead_minutes"`
}

func (h *CalendarHandler) SaveSettings(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	var request settingsRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	settings, err := h.service.SettingsOf(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	settings.UserID = userID
	settings.DigestAt = request.DigestAt
	settings.Timezone = request.Timezone
	if request.Enabled != nil {
		settings.Enabled = *request.Enabled
	}
	if request.LeadMinutes != nil {
		settings.LeadMinutes = *request.LeadMinutes
	}

	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.SaveSettings(ctx, &settings); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, newSettingsDTO(settings))
}

func (h *CalendarHandler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, calendar.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, calendar.ErrBadTime),
		errors.Is(err, calendar.ErrBadTimezone),
		errors.Is(err, calendar.ErrBadLead), errors.Is(err, calendar.ErrBadDigest),
		errors.Is(err, calendar.ErrTitleNeeded):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		h.logger.Error("calendar request failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}
