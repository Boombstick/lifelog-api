package adminui

import (
	"net/http"
	"regexp"
	"time"

	"lifelog-api/internal/visits"

	"github.com/gin-gonic/gin"
)

var visitorPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (h *Handler) visitsPage(c *gin.Context) {
	if h.visits == nil {
		h.render(c, http.StatusServiceUnavailable, "visits.html", gin.H{"Error": "Статистика посещений выключена."})
		return
	}
	now := time.Now()
	day := c.Query("day")
	if !visits.ValidDay(day) {
		day = now.UTC().Format("2006-01-02")
	}
	summary, err := h.visits.Summary(c.Request.Context(), now)
	if err != nil {
		h.render(c, http.StatusInternalServerError, "visits.html", gin.H{"Error": err.Error()})
		return
	}
	visitors, err := h.visits.Visitors(c.Request.Context(), day)
	if err != nil {
		h.render(c, http.StatusInternalServerError, "visits.html", gin.H{"Error": err.Error()})
		return
	}
	requests := 0
	for _, v := range visitors {
		requests += v.Requests
	}
	h.render(c, http.StatusOK, "visits.html", gin.H{
		"Summary":  summary,
		"Day":      day,
		"Visitors": visitors,
		"Requests": requests,
	})
}

func (h *Handler) visitorPage(c *gin.Context) {
	day, visitor := c.Param("day"), c.Param("visitor")
	if h.visits == nil || !visits.ValidDay(day) || !visitorPattern.MatchString(visitor) {
		c.Redirect(http.StatusSeeOther, h.prefix+"/visits")
		return
	}
	requests, err := h.visits.Requests(c.Request.Context(), day, visitor)
	if err != nil {
		h.render(c, http.StatusInternalServerError, "visitor.html", gin.H{"Error": err.Error()})
		return
	}
	h.render(c, http.StatusOK, "visitor.html", gin.H{
		"Day":      day,
		"Visitor":  visitor,
		"Requests": requests,
		"Keep":     visits.KeepRequests,
	})
}
