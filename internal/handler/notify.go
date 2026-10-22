package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"lifelog-api/internal/notify"

	"github.com/gin-gonic/gin"
)

type NotifyHandler struct {
	handler
	logger  *slog.Logger
	service *notify.Service
}

func NewNotifyHandler(logger *slog.Logger, service *notify.Service) *NotifyHandler {
	return &NotifyHandler{logger: logger, service: service}
}

func (h *NotifyHandler) Register(engine *gin.Engine, auth, internalGuard gin.HandlerFunc) {
	engine.GET("/notify/vapid-key", h.vapidKey)

	group := engine.Group("/notify", auth)
	group.GET("/devices", h.devices)
	group.PUT("/devices/:device_id", h.putDevice)
	group.PATCH("/devices/:device_id", h.patchDevice)
	group.DELETE("/devices/:device_id", h.deleteDevice)
	group.GET("/settings", h.settings)
	group.PUT("/settings", h.putSettings)
	group.POST("/test", h.test)

	engine.Group("/internal", internalGuard).POST("/notify", h.internalNotify)
}

type deviceRequest struct {
	Channel  string `json:"channel"`
	Token    string `json:"token"`
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Platform string `json:"platform"`
	Label    string `json:"label"`
}

type deviceToggleRequest struct {
	Enabled *bool `json:"enabled"`
}

type pushSettingsRequest struct {
	PushEnabled *bool `json:"push_enabled"`
}

type internalNotifyRequest struct {
	UserIDs     []string `json:"user_ids"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	URL         string   `json:"url"`
	CollapseKey string   `json:"collapse_key"`
	TTLSeconds  *int     `json:"ttl_seconds"`
}

var (
	errEnabledMissing     = errors.New("Укажите enabled")
	errPushEnabledMissing = errors.New("Укажите push_enabled")
)

func (h *NotifyHandler) fail(c *gin.Context, logMsg string, err error) {
	switch {
	case errors.Is(err, notify.ErrDeviceNotFound):
		h.notFoundJSON(c, err)
	case errors.Is(err, notify.ErrTooOften):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
	case errors.Is(err, notify.ErrWebPushDisabled):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, notify.ErrBadEndpoint):

		if host, ok := notify.RefusedHost(err); ok {
			h.logger.Warn("web push endpoint host refused", "host", host)
		}
		h.badRequestJSON(c, notify.ErrBadEndpoint)
	case errors.Is(err, notify.ErrBadDeviceID),
		errors.Is(err, notify.ErrBadKeys),
		errors.Is(err, notify.ErrBadChannel),
		errors.Is(err, notify.ErrEmptyToken),
		errors.Is(err, notify.ErrTokenTooLong),
		errors.Is(err, notify.ErrBadPlatform),
		errors.Is(err, notify.ErrNoRecipients),
		errors.Is(err, notify.ErrTooManyRecipients),
		errors.Is(err, notify.ErrBadRecipient),
		errors.Is(err, notify.ErrBadKind),
		errors.Is(err, notify.ErrEmptyTitle),
		errors.Is(err, notify.ErrBadURL),
		errors.Is(err, notify.ErrBadTTL),
		errors.Is(err, notify.ErrBadCollapseKey):
		h.badRequestJSON(c, err)
	default:
		h.logger.Error(logMsg, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (h *NotifyHandler) vapidKey(c *gin.Context) {
	key, err := h.service.VAPIDKey()
	if err != nil {
		h.fail(c, "vapid key not read", err)
		return
	}
	c.JSON(http.StatusOK, key)
}

func (h *NotifyHandler) devices(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	list, err := h.service.Devices(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, "push devices not read", err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *NotifyHandler) putDevice(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	var request deviceRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	err := h.service.RegisterDevice(ctx, userID, c.Param("device_id"), notify.RegisterInput{
		Channel:  request.Channel,
		Token:    request.Token,
		Endpoint: request.Endpoint,
		P256dh:   request.Keys.P256dh,
		Auth:     request.Keys.Auth,
		Platform: request.Platform,
		Label:    request.Label,
	})
	if err != nil {
		h.fail(c, "push device not registered", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *NotifyHandler) patchDevice(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	var request deviceToggleRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if request.Enabled == nil {
		h.badRequestJSON(c, errEnabledMissing)
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.SetDeviceEnabled(ctx, userID, c.Param("device_id"), *request.Enabled); err != nil {
		h.fail(c, "push device not switched", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *NotifyHandler) deleteDevice(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.DeleteDevice(ctx, userID, c.Param("device_id")); err != nil {
		h.fail(c, "push device not deleted", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *NotifyHandler) settings(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	settings, err := h.service.Settings(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, "push settings not read", err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (h *NotifyHandler) putSettings(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	var request pushSettingsRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if request.PushEnabled == nil {
		h.badRequestJSON(c, errPushEnabledMissing)
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.SetSettings(ctx, userID, *request.PushEnabled); err != nil {
		h.fail(c, "push settings not saved", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *NotifyHandler) test(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.service.Test(ctx, userID)
	if err != nil {
		h.fail(c, "test push not queued", err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func (h *NotifyHandler) internalNotify(c *gin.Context) {
	var request internalNotifyRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.service.Notify(ctx, notify.NotifyInput{
		UserIDs:     request.UserIDs,
		Kind:        request.Kind,
		Title:       request.Title,
		Body:        request.Body,
		URL:         request.URL,
		CollapseKey: request.CollapseKey,
		TTLSeconds:  request.TTLSeconds,
	})
	if err != nil {
		h.fail(c, "push not queued", err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}
