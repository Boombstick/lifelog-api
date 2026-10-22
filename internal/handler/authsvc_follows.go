package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxFollowCandidates = 1000

func (h *IdentityHandler) Following(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "id не uuid")
		return
	}
	ids, err := h.service.Following(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"following": ids})
}

func (h *IdentityHandler) BlockRelations(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "id не uuid")
		return
	}
	ids, err := h.service.BlockRelations(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"blocked": ids})
}

type followersCheckRequest struct {
	Target     string   `json:"target" binding:"required"`
	Candidates []string `json:"candidates" binding:"required"`
}

func (h *IdentityHandler) FollowersAmong(c *gin.Context) {
	var request followersCheckRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if len(request.Candidates) > maxFollowCandidates {
		badRequest(c, "не больше 1000 идентификаторов за раз")
		return
	}
	target, err := uuid.Parse(request.Target)
	if err != nil {
		badRequest(c, "target не uuid")
		return
	}
	candidates := make([]uuid.UUID, 0, len(request.Candidates))
	for _, raw := range request.Candidates {
		id, err := uuid.Parse(raw)
		if err != nil {
			badRequest(c, "candidates содержит не uuid")
			return
		}
		candidates = append(candidates, id)
	}

	ids, err := h.service.FollowersAmong(c.Request.Context(), target, candidates)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"followers": ids})
}
