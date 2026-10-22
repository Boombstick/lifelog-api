package middleware

import (
	"net/http"

	"lifelog-api/internal/auth"

	"github.com/gin-gonic/gin"
)

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := auth.PrincipalFromContext(c.Request.Context())
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if principal.Role != auth.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "требуется роль администратора"})
			return
		}
		c.Next()
	}
}
