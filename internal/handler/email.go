package handler

import (
	"net/http"
	"strings"

	"lifelog-api/internal/authsvc"

	"github.com/gin-gonic/gin"
)

type confirmRegisterRequest struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

type forgotPasswordRequest struct {
	Email string `json:"email" binding:"required"`
}

type codeResetRequest struct {
	Email       string `json:"email" binding:"required"`
	Code        string `json:"code" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

type attachEmailRequest struct {
	Email string `json:"email" binding:"required"`
}

type confirmCodeRequest struct {
	Code string `json:"code" binding:"required"`
}

func (h *IdentityHandler) SetMailbox(mailbox *authsvc.CaptureMailer) { h.mailbox = mailbox }

func (h *IdentityHandler) ConfirmRegistration(c *gin.Context) {
	var request confirmRegisterRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	pair, err := h.service.RegisterConfirm(c.Request.Context(), request.Email, request.Code, clientOf(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, pair)
}

func (h *IdentityHandler) ForgotPassword(c *gin.Context) {
	var request forgotPasswordRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if err := h.service.ForgotPassword(c.Request.Context(), request.Email); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
}

func (h *IdentityHandler) ResetPasswordWithCode(c *gin.Context) {
	var request codeResetRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if err := h.service.ResetPasswordWithCode(c.Request.Context(), request.Email, request.Code, request.NewPassword); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) AttachEmail(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}
	var request attachEmailRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if err := h.service.AttachEmailStart(c.Request.Context(), userID, request.Email); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
}

func (h *IdentityHandler) ConfirmEmail(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}
	var request confirmCodeRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if err := h.service.AttachEmailConfirm(c.Request.Context(), userID, request.Code); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) LastMail(c *gin.Context) {
	if h.mailbox == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "почта не перехватывается"})
		return
	}
	captured, ok := h.mailbox.Last(strings.TrimSpace(c.Query("to")))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "писем на этот адрес не было"})
		return
	}
	c.JSON(http.StatusOK, captured)
}
