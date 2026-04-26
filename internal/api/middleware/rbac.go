package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

func RoleBasedAuthMiddleware(minRole models.UserRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := c.Request.Context().Value(utils.ContextKeyClaims).(*models.TokenClaims)
		if !ok {
			c.JSON(403, gin.H{
				"error":   "forbidden",
				"message": "insufficient permissions",
			})
			c.Abort()
			return
		}

		if !utils.HasPermission(claims.Role, minRole) {
			c.JSON(403, gin.H{
				"error":   "forbidden",
				"message": "insufficient permissions",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
