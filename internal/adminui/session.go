package adminui

import (
	"crypto/subtle"
	"net/http"

	"lifelog-api/internal/authsvc"
	"lifelog-api/internal/randtoken"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	accessCookie  = "admin_access"
	refreshCookie = "admin_refresh"
	csrfCookie    = "admin_csrf"

	principalKey = "admin_principal"
	csrfKey      = "admin_csrf_token"
)

type principal struct {
	UserID uuid.UUID
	Name   string
	Family uuid.UUID
	Access string
}

func (h *Handler) require(c *gin.Context) {
	access, _ := c.Cookie(accessCookie)
	claims, err := h.signer.Parse(access)
	if err != nil {
		refresh, _ := c.Cookie(refreshCookie)
		if refresh == "" {
			h.toLogin(c)
			return
		}
		pair, refreshErr := h.service.Refresh(c.Request.Context(), refresh, authsvc.Client{UserAgent: c.Request.UserAgent()})
		if refreshErr != nil {
			h.clearSession(c)
			h.toLogin(c)
			return
		}
		h.setSession(c, pair)
		access = pair.AccessToken
		if claims, err = h.signer.Parse(access); err != nil {
			h.clearSession(c)
			h.toLogin(c)
			return
		}
	}

	if claims.Role != string(authsvc.RoleAdmin) {
		h.clearSession(c)
		h.render(c, http.StatusForbidden, "login.html", gin.H{
			"Error": "Эта учётная запись не администратор.",
		})
		c.Abort()
		return
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		h.clearSession(c)
		h.toLogin(c)
		return
	}
	family, _ := uuid.Parse(claims.Family)

	c.Set(principalKey, principal{UserID: userID, Name: claims.Name, Family: family, Access: access})
	token, err := h.ensureCSRF(c)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Set(csrfKey, token)
	c.Next()
}

func principalOf(c *gin.Context) principal {
	value, _ := c.Get(principalKey)
	person, _ := value.(principal)
	return person
}

func (h *Handler) toLogin(c *gin.Context) {
	c.Redirect(http.StatusSeeOther, h.prefix+"/login")
	c.Abort()
}

func (h *Handler) setSession(c *gin.Context, pair *authsvc.TokenPair) {
	h.setCookie(c, accessCookie, pair.AccessToken, int(h.accessTTL.Seconds()))
	h.setCookie(c, refreshCookie, pair.RefreshToken, int(h.refreshTTL.Seconds()))
}

func (h *Handler) clearSession(c *gin.Context) {
	for _, name := range []string{accessCookie, refreshCookie, csrfCookie} {
		h.setCookie(c, name, "", -1)
	}
}

func (h *Handler) setCookie(c *gin.Context, name, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     h.prefix,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isSecure(c),
		SameSite: http.SameSiteStrictMode,
	})
}

func isSecure(c *gin.Context) bool {
	return c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
}

func (h *Handler) ensureCSRF(c *gin.Context) (string, error) {
	if existing, err := c.Cookie(csrfCookie); err == nil && existing != "" {
		return existing, nil
	}
	token := randtoken.New(24)
	h.setCookie(c, csrfCookie, token, int(h.refreshTTL.Seconds()))
	return token, nil
}

func (h *Handler) checkCSRF(c *gin.Context) bool {
	expected, err := c.Cookie(csrfCookie)
	if err != nil || expected == "" {
		return false
	}
	got := c.PostForm("csrf")
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}
