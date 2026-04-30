package api

import (
	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/api/factories"
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/api/middleware"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/storage/postgres"
	redisstore "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

type RegisterRoutesParams struct {
	DB       *gorm.DB
	Config   *config.Config
	Router   *gin.Engine
	Cache    redisstore.Cache
	Enqueuer jobs.Enqueuer
}

func RegisterRoutes(params *RegisterRoutesParams) {
	params.Router.Use(middleware.Logger())
	params.Router.Use(middleware.ErrorHandler())

	repository := postgres.NewPostgresRepository(params.DB)
	authConfig := factories.MakeAuthConfig(repository, params.Config)
	repoHandler := factories.MakeRepositoryHandler(repository, params.Cache, params.Enqueuer)
	webhookHandler := factories.MakeWebhookHandler(repository, params.Enqueuer)
	analysisHandler := factories.MakeAnalysisHandler(repository, params.Enqueuer)
	dependencyHandler := factories.MakeDependencyHandler(repository, params.Enqueuer)
	orgConfigHandler := handlers.NewOrganizationConfigHandler(repository)

	setupAPIRoutes(params.Router, authConfig.AuthHandler, authConfig.AuthMiddleware, repoHandler, webhookHandler, analysisHandler, dependencyHandler, orgConfigHandler)
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
	repoHandler *handlers.RepositoryHandler,
	webhookHandler *handlers.WebhookHandler,
	analysisHandler *handlers.AnalysisHandler,
	dependencyHandler *handlers.DependencyHandler,
	orgConfigHandler *handlers.OrganizationConfigHandler,
) {
	// Swagger UI
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	public := router.Group("/api/v1")
	{
		public.GET("/health", healthCheck)
		public.POST("/auth/login", authHandler.LoginWithEmail)
		public.POST("/auth/select-organization", authHandler.SelectOrganization)
		public.POST("/auth/register", authHandler.RegisterWithEmail)
		public.POST("/orgs/:slug/auth/login", authHandler.LoginWithEmail)
		public.POST("/orgs/:slug/auth/register", authHandler.RegisterWithEmail)
		public.POST("/auth/refresh", authHandler.RefreshTokens)
		public.GET("/auth/:provider", authHandler.OAuthLogin)
		public.GET("/auth/:provider/callback", authHandler.OAuthCallback)
		public.GET("/orgs/:slug/auth/:provider", authHandler.OAuthLogin)
		public.GET("/orgs/:slug/auth/:provider/callback", authHandler.OAuthCallback)

		// GitHub webhook receiver — public, authenticated via HMAC signature
		public.POST("/webhooks/github/:repoID", webhookHandler.HandleGitHubWebhook)
	}

	protected := router.Group("/api/v1")
	protected.Use(authMiddleware)
	{
		protected.POST("/auth/logout", authHandler.Logout)
		protected.GET("/users/me", authHandler.GetCurrentUser)

		protected.GET("/organizations/configs", orgConfigHandler.GetConfig)
		protected.PATCH("/organizations/configs", orgConfigHandler.UpdateConfig)

		protected.POST("/repositories", repoHandler.CreateRepository)
		protected.GET("/repositories", repoHandler.ListRepositories)
		protected.GET("/repositories/:id", repoHandler.GetRepository)
		protected.PUT("/repositories/:id", repoHandler.UpdateRepository)
		protected.DELETE("/repositories/:id", repoHandler.DeleteRepository)

		// Analysis routes
		protected.POST("/repositories/:id/analyze", analysisHandler.AnalyzeRepository)
		protected.GET("/repositories/:id/analyses", analysisHandler.ListAnalyses)
		protected.POST("/repositories/:id/embeddings", analysisHandler.GenerateEmbeddings)
		protected.GET("/repositories/:id/search", analysisHandler.SemanticSearch)
		protected.POST("/repositories/:id/dependencies/scan", dependencyHandler.ScanDependencies)
		protected.GET("/repositories/:id/dependencies", dependencyHandler.ListDependencies)
	}
}
