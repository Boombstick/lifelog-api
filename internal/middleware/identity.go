package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/token"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TokenVerifier interface {
	Parse(ctx context.Context, raw string) (*token.Claims, error)
}

type PrincipalResolver interface {
	Resolve(ctx context.Context, authUserID uuid.UUID, role, name string) (auth.Principal, error)
}

func Authenticate(verifier TokenVerifier, resolver PrincipalResolver, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := BearerToken(c.GetHeader("Authorization"))
		if !ok {
			unauthorized(c)
			return
		}

		if verifier != nil && resolver != nil {
			claims, err := verifier.Parse(c.Request.Context(), raw)
			if err == nil {
				principal, err := principalFromClaims(c.Request.Context(), resolver, claims)
				if err != nil {
					logger.Error("не удалось сопоставить пользователя auth", "sub", claims.Subject, "error", err)
					unauthorized(c)
					return
				}
				withPrincipal(c, principal)
				return
			}
		}

		unauthorized(c)
	}
}

func principalFromClaims(ctx context.Context, resolver PrincipalResolver, claims *token.Claims) (auth.Principal, error) {
	authUserID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return auth.Principal{}, err
	}
	return resolver.Resolve(ctx, authUserID, claims.Role, claims.Name)
}

func withPrincipal(c *gin.Context, principal auth.Principal) {
	c.Request = c.Request.WithContext(auth.WithPrincipal(c.Request.Context(), principal))
	c.Next()
}

func unauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

func OptionalAuth(verifier TokenVerifier, resolver PrincipalResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil || resolver == nil {
			c.Next()
			return
		}
		raw, ok := BearerToken(c.GetHeader("Authorization"))
		if !ok {
			c.Next()
			return
		}
		claims, err := verifier.Parse(c.Request.Context(), raw)
		if err != nil {
			c.Next()
			return
		}
		principal, err := principalFromClaims(c.Request.Context(), resolver, claims)
		if err != nil {
			c.Next()
			return
		}
		withPrincipal(c, principal)
	}
}

func AuthenticateAccount(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := BearerToken(c.GetHeader("Authorization"))
		if !ok || verifier == nil {
			unauthorized(c)
			return
		}
		claims, err := verifier.Parse(c.Request.Context(), raw)
		if err != nil {
			unauthorized(c)
			return
		}
		id, err := uuid.Parse(claims.Subject)
		if err != nil {
			unauthorized(c)
			return
		}
		c.Set(accountIDKey, id)
		c.Next()
	}
}

const accountIDKey = "auth_user_id"

func AccountID(c *gin.Context) (uuid.UUID, bool) {
	raw, _ := c.Get(accountIDKey)
	id, ok := raw.(uuid.UUID)
	return id, ok
}
