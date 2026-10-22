package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type blockRequest struct {
	Login string `json:"login" binding:"required"`
}

func (h *IdentityHandler) Block(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}
	var request blockRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	view, err := h.service.Block(c.Request.Context(), userID, request.Login)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, view)
}

func (h *IdentityHandler) Blocks(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	blocks, err := h.service.Blocks(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": blocks})
}

func (h *IdentityHandler) Unblock(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}
	blockedID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "идентификатор пользователя должен быть uuid")
		return
	}

	if err := h.service.Unblock(c.Request.Context(), userID, blockedID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
