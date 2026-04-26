package factories

import (
	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/api/middleware"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

type AuthHandlerConfigResponse struct {
	AuthHandler    *handlers.AuthHandler
	AuthMiddleware func(c *gin.Context)
}

func MakeAuthConfig(
	repo storage.Repository,
	config *config.Config,
) *AuthHandlerConfigResponse {
	authService := services.NewAuthService(
		repo,
		config.Server.JWTSecret,
		config.Server.JWTIssuer,
		config.Server.JWTAudience,
		config.Server.AccessTokenTTL,
		config.Server.RefreshTokenTTL,
	)

	return &AuthHandlerConfigResponse{
		AuthHandler:    handlers.NewAuthHandler(authService),
		AuthMiddleware: middleware.AuthMiddleware(authService),
	}
}
