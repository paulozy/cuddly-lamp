package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

type Repository interface {
	// User operations
	GetUser(ctx context.Context, id string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateUser(ctx context.Context, user *models.User) error
	UpdateUser(ctx context.Context, user *models.User) error
	ListUsers(ctx context.Context, limit, offset int) ([]models.User, error)

	// Organization operations
	GetOrganization(ctx context.Context, id string) (*models.Organization, error)
	GetOrganizationBySlug(ctx context.Context, slug string) (*models.Organization, error)
	CreateOrganization(ctx context.Context, org *models.Organization) error
	GetOrganizationMember(ctx context.Context, orgID, userID string) (*models.OrganizationMember, error)
	ListOrganizationMembersForUser(ctx context.Context, userID string) ([]models.OrganizationMember, error)
	CreateOrganizationMember(ctx context.Context, member *models.OrganizationMember) error
	CountOrganizationMembers(ctx context.Context, orgID string) (int64, error)
	GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error)
	UpsertOrganizationConfig(ctx context.Context, cfg *models.OrganizationConfig) error

	// Repository operations
	GetRepository(ctx context.Context, id string) (*models.Repository, error)
	GetRepositoryByURL(ctx context.Context, organizationID, url string) (*models.Repository, error)
	CreateRepository(ctx context.Context, repo *models.Repository) error
	UpdateRepository(ctx context.Context, repo *models.Repository) error
	ListRepositories(ctx context.Context, filter *RepositoryFilter) ([]models.Repository, int64, error)
	DeleteRepository(ctx context.Context, id string) error
	SearchRepositories(ctx context.Context, query string, limit, offset int) ([]models.Repository, error)

	// Repository relationship operations
	CreateRepositoryRelationship(ctx context.Context, rel *models.RepositoryRelationship) error
	GetRepositoryRelationship(ctx context.Context, id string) (*models.RepositoryRelationship, error)
	UpdateRepositoryRelationship(ctx context.Context, rel *models.RepositoryRelationship) error
	DeleteRepositoryRelationship(ctx context.Context, id string) error
	ListRepositoryRelationships(ctx context.Context, filter RepositoryRelationshipFilter) ([]models.RepositoryRelationship, error)

	// Webhook operations
	GetWebhook(ctx context.Context, id string) (*models.Webhook, error)
	GetWebhookByDeliveryID(ctx context.Context, deliveryID string) (*models.Webhook, error)
	CreateWebhook(ctx context.Context, webhook *models.Webhook) error
	UpdateWebhook(ctx context.Context, webhook *models.Webhook) error
	ListPendingWebhooks(ctx context.Context, limit int) ([]models.Webhook, error)
	ListFailedWebhooks(ctx context.Context, limit, offset int) ([]models.Webhook, error)

	// WebhookConfig operations
	GetWebhookConfigByRepoID(ctx context.Context, repoID string) (*models.WebhookConfig, error)
	CreateWebhookConfig(ctx context.Context, cfg *models.WebhookConfig) error
	UpdateWebhookConfig(ctx context.Context, cfg *models.WebhookConfig) error

	// Code Analysis operations
	GetCodeAnalysis(ctx context.Context, id string) (*models.CodeAnalysis, error)
	CreateCodeAnalysis(ctx context.Context, analysis *models.CodeAnalysis) error
	UpdateCodeAnalysis(ctx context.Context, analysis *models.CodeAnalysis) error
	GetAnalysesByRepository(ctx context.Context, repoID string, limit, offset int) ([]models.CodeAnalysis, int64, error)
	ListAnalyses(ctx context.Context, repoID string, limit, offset int) ([]models.CodeAnalysis, int64, error)
	GetLatestAnalysis(ctx context.Context, repoID string, analysisType models.AnalysisType) (*models.CodeAnalysis, error)
	GetLatestAnalysisForPullRequest(ctx context.Context, repoID string, pullRequestID int, analysisType models.AnalysisType) (*models.CodeAnalysis, error)
	ListLatestAnalysesForPullRequests(ctx context.Context, repoID string, pullRequestIDs []int, analysisType models.AnalysisType) (map[int]models.CodeAnalysis, error)
	GetRepositoriesNeedingAnalysis(ctx context.Context, limit int) ([]models.Repository, error)
	SumTokensUsedSince(ctx context.Context, organizationID string, since time.Time) (int64, error)

	// Documentation generation operations
	CreateDocGeneration(ctx context.Context, doc *models.DocGeneration) error
	UpdateDocGeneration(ctx context.Context, doc *models.DocGeneration) error
	GetDocGeneration(ctx context.Context, id string) (*models.DocGeneration, error)
	GetLatestDocGenerationForRepo(ctx context.Context, repoID string) (*models.DocGeneration, error)
	ListDocGenerationsForRepo(ctx context.Context, repoID string) ([]models.DocGeneration, error)

	// Code Template operations
	CreateCodeTemplate(ctx context.Context, template *models.CodeTemplate) error
	GetCodeTemplate(ctx context.Context, id string) (*models.CodeTemplate, error)
	UpdateCodeTemplate(ctx context.Context, template *models.CodeTemplate) error
	ListCodeTemplates(ctx context.Context, filter CodeTemplateFilter) ([]models.CodeTemplate, int64, error)

	// Package Dependency operations
	UpsertPackageDependency(ctx context.Context, dep *models.PackageDependency) error
	ListPackageDependencies(ctx context.Context, repoID string, onlyVulnerable bool) ([]*models.PackageDependency, error)
	UpdatePackageDependencyVulnStatus(ctx context.Context, id string, isVulnerable bool, cves []string, latestVersion string) error
	DeletePackageDependencies(ctx context.Context, repoID string) error

	// Coverage upload operations
	CreateCoverageUpload(ctx context.Context, upload *models.CoverageUpload) error
	GetLatestCoverageUpload(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error)
	ListCoverageUploadsForCommit(ctx context.Context, repoID, sha string) ([]*models.CoverageUpload, error)
	PatchCodeAnalysisCoverage(ctx context.Context, repoID, sha string, covered, total int, percentage float64, status string) (int64, error)

	// Coverage upload tokens
	CreateCoverageUploadToken(ctx context.Context, token *models.CoverageUploadToken) error
	GetCoverageUploadTokenByHash(ctx context.Context, hash string) (*models.CoverageUploadToken, error)
	GetCoverageUploadToken(ctx context.Context, id string) (*models.CoverageUploadToken, error)
	ListCoverageUploadTokens(ctx context.Context, repoID string) ([]*models.CoverageUploadToken, error)
	RevokeCoverageUploadToken(ctx context.Context, id string) error
	TouchCoverageUploadTokenUsage(ctx context.Context, id string) error

	// Code Embedding operations
	CreateCodeEmbedding(ctx context.Context, embedding *models.CodeEmbedding) error
	CreateCodeEmbeddings(ctx context.Context, embeddings []models.CodeEmbedding) error
	SearchEmbeddings(ctx context.Context, filter EmbeddingSearchFilter) ([]models.CodeEmbedding, error)
	DeleteEmbeddings(ctx context.Context, filter EmbeddingDeleteFilter) error

	// Token operations
	CreateToken(ctx context.Context, token *models.Token) error
	GetTokenByJTI(ctx context.Context, jti string) (*models.Token, error)
	GetTokenByHash(ctx context.Context, tokenHash string) (*models.Token, error)
	RevokeToken(ctx context.Context, jti string, reason string) error
	RevokeTokenFamily(ctx context.Context, familyID uuid.UUID, reason string) error
	UpdateTokenLastUsed(ctx context.Context, jti string) error

	// OAuth operations
	GetOAuthConnection(ctx context.Context, provider, providerUserID string) (*models.OAuthConnection, error)
	UpsertOAuthConnection(ctx context.Context, conn *models.OAuthConnection) error
}

type RepositoryFilter struct {
	OrganizationID string
	OwnerUserID    string
	Type           models.RepositoryType
	IsPublic       bool
	AnalysisStatus string
	SearchQuery    string
	Limit          int
	Offset         int
}

type RepositoryRelationshipFilter struct {
	OrganizationID string
	RepositoryID   string
	Kind           models.RepositoryRelationshipKind
	Source         models.RepositoryRelationshipSource
}

type CodeTemplateFilter struct {
	OrganizationID string
	RepositoryID   string
	IsPinned       *bool
	Status         string
	Limit          int
	Offset         int
}

type EmbeddingSearchFilter struct {
	RepositoryID string
	Query        string
	Vector       []float32
	Provider     string
	Model        string
	Dimension    int
	Branch       string
	Limit        int
	MinScore     float64
}

type EmbeddingDeleteFilter struct {
	RepositoryID string
	Provider     string
	Model        string
	Dimension    int
	Branch       string
}
