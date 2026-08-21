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
	ListOrganizationMembers(ctx context.Context, orgID string) ([]models.OrganizationMember, error)
	CountOrganizationAdmins(ctx context.Context, orgID string) (int64, error)
	UpdateOrganizationMemberRole(ctx context.Context, orgID, userID string, role models.UserRole) error
	DeleteOrganizationMember(ctx context.Context, orgID, userID string) error
	GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error)
	UpsertOrganizationConfig(ctx context.Context, cfg *models.OrganizationConfig) error

	// Organization invite operations — the gate for joining an existing org.
	CreateOrganizationInvite(ctx context.Context, invite *models.OrganizationInvite) error
	GetOrganizationInviteByHash(ctx context.Context, hash string) (*models.OrganizationInvite, error)
	GetOrganizationInvite(ctx context.Context, id string) (*models.OrganizationInvite, error)
	ListOrganizationInvites(ctx context.Context, orgID string) ([]models.OrganizationInvite, error)
	RevokeOrganizationInvite(ctx context.Context, id string) error
	// AcceptOrganizationInvite marks the invite spent and creates the membership in
	// one transaction, so a crash between the two cannot leave a redeemed invite
	// without a member, or a member admitted by an invite still marked pending.
	AcceptOrganizationInvite(ctx context.Context, inviteID string, member *models.OrganizationMember) error

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

	// AI token accounting — budgets documentation generation, the only
	// remaining LLM-backed feature.
	SumTokensUsedSince(ctx context.Context, organizationID string, since time.Time) (int64, error)

	// Documentation generation operations
	CreateDocGeneration(ctx context.Context, doc *models.DocGeneration) error
	UpdateDocGeneration(ctx context.Context, doc *models.DocGeneration) error
	GetDocGeneration(ctx context.Context, id string) (*models.DocGeneration, error)
	GetLatestDocGenerationForRepo(ctx context.Context, repoID string) (*models.DocGeneration, error)
	ListDocGenerationsForRepo(ctx context.Context, repoID string) ([]models.DocGeneration, error)
	// Org-scope helpers (scope = 'org')
	ListOrgDocGenerations(ctx context.Context, orgID string) ([]models.DocGeneration, error)
	GetLatestOrgDocs(ctx context.Context, orgID string, types []string) ([]models.DocGeneration, error)


	// Maintenance / startup recovery
	ResetStaleSyncingRepositories(ctx context.Context) ([]string, error)

	// Coverage upload operations
	CreateCoverageUpload(ctx context.Context, upload *models.CoverageUpload) error
	GetLatestCoverageUpload(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error)
	ListCoverageUploadsForCommit(ctx context.Context, repoID, sha string) ([]*models.CoverageUpload, error)

	// Coverage upload tokens
	CreateCoverageUploadToken(ctx context.Context, token *models.CoverageUploadToken) error
	GetCoverageUploadTokenByHash(ctx context.Context, hash string) (*models.CoverageUploadToken, error)
	GetCoverageUploadToken(ctx context.Context, id string) (*models.CoverageUploadToken, error)
	ListCoverageUploadTokens(ctx context.Context, repoID string) ([]*models.CoverageUploadToken, error)
	RevokeCoverageUploadToken(ctx context.Context, id string) error
	TouchCoverageUploadTokenUsage(ctx context.Context, id string) error

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
