package api

import (
	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/api/factories"
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/api/middleware"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
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
	relationshipHandler := factories.MakeRepositoryRelationshipHandler(repository)
	webhookHandler := factories.MakeWebhookHandler(repository, params.Enqueuer)
	pullRequestHandler := factories.MakePullRequestHandler(repository)
	docsHandler := factories.MakeDocsHandler(repository, params.Enqueuer)
	orgConfigHandler := handlers.NewOrganizationConfigHandler(repository)
	coverageHandler := factories.MakeCoverageHandler(repository)
	memberHandler := factories.MakeOrganizationMemberHandler(repository)

	setupAPIRoutes(params.Router, authConfig.AuthHandler, authConfig.AuthMiddleware, repoHandler, relationshipHandler, webhookHandler, pullRequestHandler, docsHandler, orgConfigHandler, coverageHandler, memberHandler)
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
	relationshipHandler *handlers.RepositoryRelationshipHandler,
	webhookHandler *handlers.WebhookHandler,
	pullRequestHandler *handlers.PullRequestHandler,
	docsHandler *handlers.DocsHandler,
	orgConfigHandler *handlers.OrganizationConfigHandler,
	coverageHandler *handlers.CoverageHandler,
	memberHandler *handlers.OrganizationMemberHandler,
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

		// Coverage upload — public, authenticated via Bearer cov_* token
		// (validated inside the handler, not by the JWT middleware).
		public.POST("/repositories/:id/coverage", coverageHandler.IngestCoverage)
	}

	protected := router.Group("/api/v1")
	protected.Use(authMiddleware)
	{
		protected.POST("/auth/logout", authHandler.Logout)
		protected.GET("/users/me", authHandler.GetCurrentUser)

		protected.GET("/organizations/configs", orgConfigHandler.GetConfig)
		protected.PATCH("/organizations/configs", orgConfigHandler.UpdateConfig)

		// Role gates. Reads are open to any member; writes escalate with the
		// blast radius of the action. The service layer still enforces that the
		// resource belongs to the caller's organization — these two checks are
		// complementary, not redundant.
		developer := middleware.RequireRole(models.RoleDeveloper)
		maintainer := middleware.RequireRole(models.RoleMaintainer)
		admin := middleware.RequireRole(models.RoleAdmin)

		// Membership is an admin concern: who is in the organization, and who
		// gets invited in.
		protected.GET("/organizations/members", admin, memberHandler.ListMembers)
		protected.PATCH("/organizations/members/:userID", admin, memberHandler.UpdateMemberRole)
		protected.DELETE("/organizations/members/:userID", admin, memberHandler.RemoveMember)
		protected.POST("/organizations/invites", admin, memberHandler.CreateInvite)
		protected.GET("/organizations/invites", admin, memberHandler.ListInvites)
		protected.DELETE("/organizations/invites/:id", admin, memberHandler.RevokeInvite)

		protected.POST("/repositories", developer, repoHandler.CreateRepository)
		protected.GET("/repositories", repoHandler.ListRepositories)
		protected.GET("/repositories/graph", relationshipHandler.GetGraph)
		protected.GET("/repositories/:id", repoHandler.GetRepository)
		protected.POST("/repositories/:id/sync", developer, repoHandler.SyncRepository)
		protected.PUT("/repositories/:id", maintainer, repoHandler.UpdateRepository)
		protected.DELETE("/repositories/:id", maintainer, repoHandler.DeleteRepository)
		protected.POST("/repository-relationships", developer, relationshipHandler.CreateRelationship)
		protected.PATCH("/repository-relationships/:id", developer, relationshipHandler.UpdateRelationship)
		protected.DELETE("/repository-relationships/:id", developer, relationshipHandler.DeleteRelationship)

		// Pull request routes (read-only GitHub pass-through)
		protected.GET("/repositories/:id/pull-requests", pullRequestHandler.ListPullRequests)
		protected.GET("/repositories/:id/pull-requests/:pr_number", pullRequestHandler.GetPullRequest)
		protected.GET("/repositories/:id/pull-requests/:pr_number/files", pullRequestHandler.GetPullRequestFiles)

		// Doc generation spends the organization's Anthropic budget, so it is
		// gated even though the result is harmless.
		protected.POST("/repositories/:id/docs/generate", developer, docsHandler.GenerateRepositoryDocs)
		protected.GET("/repositories/:id/docs", docsHandler.ListRepositoryDocs)
		protected.GET("/docs/:id", docsHandler.GetDocGeneration)
		protected.PATCH("/docs/:id", developer, docsHandler.UpdateDocContent)
		protected.GET("/docs/templates", docsHandler.ListDocTemplates)
		// GenerateOrgDocs/ListOrgDocs additionally require admin inside the
		// handler (requireOrgAdmin) — org-wide documents are an admin concern.
		protected.POST("/organizations/docs/generate", docsHandler.GenerateOrgDocs)
		protected.GET("/organizations/docs", docsHandler.ListOrgDocs)

		// Coverage upload tokens are credentials — minting and revoking them is
		// a maintainer action.
		protected.POST("/repositories/:id/coverage/tokens", maintainer, coverageHandler.CreateCoverageToken)
		protected.GET("/repositories/:id/coverage/tokens", maintainer, coverageHandler.ListCoverageTokens)
		protected.DELETE("/repositories/:id/coverage/tokens/:tokenID", maintainer, coverageHandler.RevokeCoverageToken)
	}
}
