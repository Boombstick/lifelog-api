package handler

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"lifelog-api/internal/calendar"

	"github.com/gin-gonic/gin"
)

func (h *CalendarHandler) registerGoogle(engine *gin.Engine, group *gin.RouterGroup) {
	group.GET("/google", h.GoogleStatus)
	group.POST("/google/connect", h.GoogleConnect)
	group.POST("/google/sync", h.GoogleSync)
	group.DELETE("/google", h.GoogleDisconnect)

	engine.GET("/calendar/google/callback", h.GoogleCallback)
}

func (h *CalendarHandler) GoogleStatus(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	status, err := h.google.Status(c.Request.Context(), userID)
	if err != nil {
		h.failGoogle(c, err)
		return
	}
	c.JSON(http.StatusOK, status)
}

func (h *CalendarHandler) GoogleConnect(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	url, err := h.google.Connect(c.Request.Context(), userID)
	if err != nil {
		h.failGoogle(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url})
}

func (h *CalendarHandler) GoogleSync(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	count, err := h.google.Sync(c.Request.Context(), userID)
	if err != nil {
		h.failGoogle(c, err)
		return
	}
	status, err := h.google.Status(c.Request.Context(), userID)
	if err != nil {
		h.failGoogle(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported": count, "status": status})
}

func (h *CalendarHandler) GoogleDisconnect(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.google.Disconnect(ctx, userID); err != nil {
		h.failGoogle(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

var callbackPage = template.Must(template.New("callback").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>LifeLog — Google Календарь</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;line-height:1.5}</style>
</head><body><h1>{{.Title}}</h1><p>{{.Text}}</p><p>Можно вернуться в приложение.</p></body></html>`))

type callbackView struct {
	Title string
	Text  string
}

func (h *CalendarHandler) GoogleCallback(c *gin.Context) {
	render := func(status int, view callbackView) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Referrer-Policy", "no-referrer")
		c.Status(status)
		if err := callbackPage.Execute(c.Writer, view); err != nil {
			h.logger.Error("callback page failed to render", "error", err)
		}
	}

	if reason := c.Query("error"); reason != "" {
		render(http.StatusOK, callbackView{"Не подключено", "Доступ к календарю не выдан."})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()

	count, syncErr, err := h.google.Complete(ctx, c.Query("state"), c.Query("code"))
	switch {
	case errors.Is(err, calendar.ErrGoogleBadState):
		render(http.StatusBadRequest, callbackView{"Ссылка устарела", "Начните подключение в приложении ещё раз."})
	case errors.Is(err, calendar.ErrGoogleScopeMissing):
		render(http.StatusBadRequest, callbackView{"Нет доступа к событиям",
			"Подключите снова и оставьте отмеченным пункт про просмотр событий календаря."})
	case errors.Is(err, calendar.ErrGoogleDisabled):
		render(http.StatusServiceUnavailable, callbackView{"Недоступно", "Импорт из Google на сервере не настроен."})
	case err != nil:
		h.logger.Error("google callback failed", "error", err)
		render(http.StatusBadGateway, callbackView{"Не получилось", "Google не подтвердил подключение. Попробуйте ещё раз."})
	case syncErr != nil:
		h.logger.Warn("first google sync failed", "error", syncErr)
		render(http.StatusOK, callbackView{"Календарь подключён", "События подтянутся в течение получаса."})
	default:
		render(http.StatusOK, callbackView{"Календарь подключён", "Импортировано событий: " + strconv.Itoa(count) + "."})
	}
}

func (h *CalendarHandler) failGoogle(c *gin.Context, err error) {
	switch {
	case errors.Is(err, calendar.ErrGoogleDisabled):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, calendar.ErrGoogleNotLinked):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, calendar.ErrGoogleRevoked), errors.Is(err, calendar.ErrGoogleScopeMissing):

		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		h.logger.Error("google calendar request failed", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Google Календарь сейчас недоступен"})
	}
}
