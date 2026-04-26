package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

func OptionalAuthMiddleware(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := utils.ExtractToken(c)
		if err == nil {
			if claims, err := authService.ValidateToken(c.Request.Context(), token); err == nil {
				ctx := context.WithValue(c.Request.Context(), utils.ContextKeyUser, claims.UserID)
				ctx = context.WithValue(ctx, utils.ContextKeyClaims, claims)
				c.Request = c.Request.WithContext(ctx)
			}
		}
		c.Next()
	}
}
