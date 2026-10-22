package handler

import (
	"log/slog"
	"net/http"

	"lifelog-api/internal/visits"

	"github.com/gin-gonic/gin"
)

func VisitsIngest(sink visits.Sink, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var batch visits.Batch
		if !bindJSONRequest(c, &batch) {
			return
		}
		if err := sink.Record(c.Request.Context(), batch.Events); err != nil {
			logger.ErrorContext(c, "visits not recorded", "count", len(batch.Events), "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
			return
		}
		c.Status(http.StatusNoContent)
	}
}
