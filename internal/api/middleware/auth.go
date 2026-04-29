package middleware

import (
	"context"
	"fmt"

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
			fmt.Printf("Token validation error: %v\n", err)
			c.JSON(401, gin.H{
				"error":   "unauthorized",
				"message": fmt.Sprintf("invalid or expired token: %v", err),
			})
			c.Abort()
			return
		}

		ctx := context.WithValue(c.Request.Context(), utils.ContextKeyUser, claims.UserID)
		ctx = context.WithValue(ctx, utils.ContextKeyOrganization, claims.OrganizationID)
		ctx = context.WithValue(ctx, utils.ContextKeyClaims, claims)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
