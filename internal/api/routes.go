package api

import (
	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/api/factories"
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/api/middleware"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/storage/postgres"
	"gorm.io/gorm"
)

type RegisterRoutesParams struct {
	DB     *gorm.DB
	Config *config.Config
	Router *gin.Engine
}

func RegisterRoutes(params *RegisterRoutesParams) {
	params.Router.Use(middleware.Logger())
	params.Router.Use(middleware.ErrorHandler())

	repository := postgres.NewPostgresRepository(params.DB)
	authConfig := factories.MakeAuthConfig(repository, params.Config)

	setupAPIRoutes(params.Router, authConfig.AuthHandler, authConfig.AuthMiddleware)
}

func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":  "ok",
		"service": "IDP Backend",
	})
}

func setupAPIRoutes(
	router *gin.Engine,
	authHandler *handlers.AuthHandler,
	authMiddleware gin.HandlerFunc,
) {
	public := router.Group("/api/v1")
	{
		public.GET("/health", healthCheck)
		public.POST("/auth/login", authHandler.LoginWithEmail)
		public.POST("/auth/register", authHandler.RegisterWithEmail)
		public.POST("/auth/refresh", authHandler.RefreshTokens)
		public.GET("/auth/:provider", authHandler.OAuthLogin)
		public.GET("/auth/:provider/callback", authHandler.OAuthCallback)
	}

	protected := router.Group("/api/v1")
	protected.Use(authMiddleware)
	{
		protected.POST("/auth/logout", authHandler.Logout)
		protected.GET("/users/me", authHandler.GetCurrentUser)
	}
}
