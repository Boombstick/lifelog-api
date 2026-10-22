package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"lifelog-api/internal/authsvc"
	"lifelog-api/internal/middleware"
	"lifelog-api/internal/token"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	principalKey = "auth_principal"
	familyKey    = "auth_family"
	roleKey      = "auth_role"
)

type IdentityHandler struct {
	logger  *slog.Logger
	service *authsvc.Service
	signer  *token.Signer
	mailbox *authsvc.CaptureMailer
}

func NewIdentityHandler(logger *slog.Logger, service *authsvc.Service, signer *token.Signer) *IdentityHandler {
	return &IdentityHandler{logger: logger, service: service, signer: signer}
}

type registerRequest struct {
	Email       string `json:"email" binding:"required"`
	Login       string `json:"login" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Login    string `json:"login"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

func (h *IdentityHandler) Register(c *gin.Context) {
	var request registerRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	expires, err := h.service.RegisterStart(c.Request.Context(), request.Email, request.Login, request.Password, request.DisplayName)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"expires_at": expires})
}

func (h *IdentityHandler) Login(c *gin.Context) {
	var request loginRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	var pair *authsvc.TokenPair
	var err error
	switch {
	case request.Email != "":
		pair, err = h.service.LoginByEmail(c.Request.Context(), request.Email, request.Password, clientOf(c))
	case request.Login != "":
		pair, err = h.service.SignIn(c.Request.Context(), request.Login, request.Password, clientOf(c))
	default:
		badRequest(c, "нужен email или логин")
		return
	}
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pair)
}

const DeviceIDHeader = "X-Device-Id"

func clientOf(c *gin.Context) authsvc.Client {
	return authsvc.Client{
		UserAgent: c.Request.UserAgent(),
		DeviceID:  strings.TrimSpace(c.GetHeader(DeviceIDHeader)),
	}
}

func (h *IdentityHandler) Refresh(c *gin.Context) {
	var request refreshRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	pair, err := h.service.Refresh(c.Request.Context(), request.RefreshToken, clientOf(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pair)
}

func (h *IdentityHandler) Logout(c *gin.Context) {
	var request refreshRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	if err := h.service.Logout(c.Request.Context(), request.RefreshToken); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) Me(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	user, err := h.service.Me(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeProfile(c, user)
}

type updateMeRequest struct {
	DisplayName *string `json:"display_name"`
}

func (h *IdentityHandler) UpdateMe(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	var request updateMeRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if request.DisplayName == nil {
		badRequest(c, "нужно поле display_name")
		return
	}

	user, err := h.service.SetDisplayName(c.Request.Context(), userID, *request.DisplayName)
	if err != nil {
		h.fail(c, err)
		return
	}
	h.writeProfile(c, user)
}

func (h *IdentityHandler) writeProfile(c *gin.Context, user *authsvc.User) {
	telegramLinked, err := h.service.HasTelegram(c.Request.Context(), user.ID)
	if err != nil {
		h.fail(c, err)
		return
	}

	login := ""
	if user.Login != nil {
		login = *user.Login
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           user.ID,
		"login":        login,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"is_private":   user.IsPrivate,

		"telegram_linked": telegramLinked,
	})
}

func (h *IdentityHandler) ChangePassword(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	var request changePasswordRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	keep, _ := uuid.Parse(familyOf(c))
	if err := h.service.ChangePassword(c.Request.Context(), userID, request.CurrentPassword, request.NewPassword, keep); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) StartTelegramLink(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	invitation, err := h.service.StartTelegramLink(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, invitation)
}

func (h *IdentityHandler) UnlinkTelegram(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	if err := h.service.UnlinkTelegram(c.Request.Context(), userID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) JWKS(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.signer.JWKS())
}

type ensureTelegramRequest struct {
	TelegramID  int64  `json:"telegram_id" binding:"required"`
	DisplayName string `json:"display_name"`
}

type confirmLinkRequest struct {
	Code       string `json:"code" binding:"required"`
	TelegramID int64  `json:"telegram_id" binding:"required"`
}

func (h *IdentityHandler) EnsureTelegramUser(c *gin.Context) {
	var request ensureTelegramRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	user, err := h.service.EnsureTelegramUser(c.Request.Context(), request.TelegramID, request.DisplayName)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": user.ID, "display_name": user.DisplayName})
}

func (h *IdentityHandler) ResolveTelegram(c *gin.Context) {
	telegramID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		badRequest(c, "идентификатор телеграма должен быть числом")
		return
	}

	user, err := h.service.ResolveTelegram(c.Request.Context(), telegramID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": user.ID, "display_name": user.DisplayName})
}

func (h *IdentityHandler) ConfirmTelegramLink(c *gin.Context) {
	var request confirmLinkRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	user, err := h.service.ConfirmTelegramLink(c.Request.Context(), request.Code, request.TelegramID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": user.ID, "display_name": user.DisplayName})
}

func (h *IdentityHandler) ResumeMerges(c *gin.Context) {
	if err := h.service.ResumeMerges(c.Request.Context()); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) RequireToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, found := middleware.BearerToken(c.GetHeader("Authorization"))
		if !found {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "нужен токен"})
			return
		}

		claims, err := h.signer.Parse(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "токен недействителен"})
			return
		}
		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "токен недействителен"})
			return
		}

		c.Set(principalKey, userID)
		c.Set(familyKey, claims.Family)
		c.Set(roleKey, claims.Role)
		c.Next()
	}
}

func RequireInternalToken(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !middleware.InternalTokenMatches(c.GetHeader(middleware.InternalTokenHeader), expected) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "служебный токен неверен"})
			return
		}
		c.Next()
	}
}

func principalOf(c *gin.Context) (uuid.UUID, bool) {
	value, exists := c.Get(principalKey)
	if !exists {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "нужен токен"})
		return uuid.Nil, false
	}
	userID, ok := value.(uuid.UUID)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "нужен токен"})
		return uuid.Nil, false
	}
	return userID, true
}

func (h *IdentityHandler) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		value, _ := c.Get(roleKey)
		if role, _ := value.(string); role != string(authsvc.RoleAdmin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "нужны права администратора"})
			return
		}
		c.Next()
	}
}

func familyOf(c *gin.Context) string {
	value, _ := c.Get(familyKey)
	family, _ := value.(string)
	return family
}

func badRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": message})
}

func (h *IdentityHandler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authsvc.ErrLoginTaken):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrInvalidCredentials), errors.Is(err, authsvc.ErrNoPassword):
		c.JSON(http.StatusUnauthorized, gin.H{"error": authsvc.ErrInvalidCredentials.Error()})
	case errors.Is(err, authsvc.ErrRefreshInvalid), errors.Is(err, authsvc.ErrRefreshReused):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrSelfSubscription), errors.Is(err, authsvc.ErrSelfBlock):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrUserBlocked):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrSubscriptionAbsent):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrWeakPassword), errors.Is(err, authsvc.ErrNonceInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrUserNotFound), errors.Is(err, authsvc.ErrSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrInvalidEmail), errors.Is(err, authsvc.ErrLoginRequired), errors.Is(err, authsvc.ErrCodeInvalid),
		errors.Is(err, authsvc.ErrDisplayNameLength), errors.Is(err, authsvc.ErrDisplayNameControl),
		errors.Is(err, authsvc.ErrLoginHasAt), errors.Is(err, authsvc.ErrLoginLength), errors.Is(err, authsvc.ErrLoginSymbols):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrCodeTooSoon), errors.Is(err, authsvc.ErrCodeLocked), errors.Is(err, authsvc.ErrAttachLimit),
		errors.Is(err, authsvc.ErrLoginLocked):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrEmailTaken),
		errors.Is(err, authsvc.ErrTelegramAlreadyLinked), errors.Is(err, authsvc.ErrTelegramTaken):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrManagedPassword):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, authsvc.ErrTelegramNotLinked):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		h.logger.ErrorContext(c, "auth request failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (h *IdentityHandler) Sessions(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	sessions, err := h.service.Sessions(c.Request.Context(), userID, familyOf(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": sessions})
}

func (h *IdentityHandler) RevokeSession(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	familyID, err := uuid.Parse(c.Param("family"))
	if err != nil {
		badRequest(c, "идентификатор устройства должен быть uuid")
		return
	}

	if err := h.service.RevokeSession(c.Request.Context(), userID, familyID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) RevokeAllSessions(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	if err := h.service.RevokeAllSessions(c.Request.Context(), userID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
}

func (h *IdentityHandler) ResetPassword(c *gin.Context) {
	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "идентификатор пользователя должен быть uuid")
		return
	}

	var request resetPasswordRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	if err := h.service.ResetPassword(c.Request.Context(), targetID, request.NewPassword); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type subscribeRequest struct {
	Login string `json:"login" binding:"required"`
}

type privacyRequest struct {
	IsPrivate bool `json:"is_private"`
}

func (h *IdentityHandler) Subscribe(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	var request subscribeRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	view, err := h.service.Subscribe(c.Request.Context(), userID, request.Login)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, view)
}

func (h *IdentityHandler) Unsubscribe(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}
	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "идентификатор должен быть uuid")
		return
	}

	if err := h.service.Unsubscribe(c.Request.Context(), userID, targetID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) Subscriptions(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	views, err := h.service.Subscriptions(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": views})
}

func (h *IdentityHandler) Subscribers(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	views, err := h.service.Subscribers(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": views})
}

func (h *IdentityHandler) ApproveSubscriber(c *gin.Context) {
	h.decideSubscriber(c, h.service.ApproveSubscriber)
}

func (h *IdentityHandler) DeclineSubscriber(c *gin.Context) {
	h.decideSubscriber(c, h.service.DeclineSubscriber)
}

func (h *IdentityHandler) decideSubscriber(c *gin.Context, decide func(context.Context, uuid.UUID, uuid.UUID) error) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}
	followerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "идентификатор должен быть uuid")
		return
	}

	if err := decide(c.Request.Context(), userID, followerID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) SetPrivacy(c *gin.Context) {
	userID, ok := principalOf(c)
	if !ok {
		return
	}

	var request privacyRequest
	if !bindJSONRequest(c, &request) {
		return
	}

	if err := h.service.SetPrivate(c.Request.Context(), userID, request.IsPrivate); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type telegramLookupRequest struct {
	UserIDs []string `json:"user_ids" binding:"required"`
}

func (h *IdentityHandler) TelegramIDs(c *gin.Context) {
	var request telegramLookupRequest
	if !bindJSONRequest(c, &request) {
		return
	}
	if len(request.UserIDs) > 1000 {
		badRequest(c, "не больше 1000 идентификаторов за раз")
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

	links, err := h.service.TelegramIDs(c.Request.Context(), ids)
	if err != nil {
		h.fail(c, err)
		return
	}

	out := make(map[string]int64, len(links))
	for id, telegramID := range links {
		out[id.String()] = telegramID
	}
	c.JSON(http.StatusOK, gin.H{"links": out})
}
