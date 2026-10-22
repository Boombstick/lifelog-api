package handler

import (
	"encoding/json"
	"net/http"

	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
)

func (h *TripsHandler) sync(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req trips.SyncRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Sync.Process(ctx, actor, req)
	if err != nil {
		h.fail(c, err)
		return
	}
	if result.Results == nil {
		result.Results = []json.RawMessage{}
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) merge(c *gin.Context) {
	var req trips.MergeRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.services.Merge.Merge(ctx, req); err != nil {
		h.fail(c, err)
		return
	}
	noContent(c)
}

func (h *TripsHandler) identify(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.services.Identity.Identify(actor))
}
