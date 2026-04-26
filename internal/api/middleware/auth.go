package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

func AuthMiddleware(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := utils.ExtractToken(c)
		if err != nil {
			c.JSON(401, gin.H{
				"error":   "unauthorized",
				"message": err.Error(),
			})
			c.Abort()
			return
		}

		claims, err := authService.ValidateToken(c.Request.Context(), token)
		if err != nil {
			c.JSON(401, gin.H{
				"error":   "unauthorized",
				"message": "invalid or expired token",
			})
			c.Abort()
			return
		}

		ctx := context.WithValue(c.Request.Context(), utils.ContextKeyUser, claims.UserID)
		ctx = context.WithValue(ctx, utils.ContextKeyClaims, claims)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
