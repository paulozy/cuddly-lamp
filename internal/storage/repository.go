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

	// Repository operations
	GetRepository(ctx context.Context, id string) (*models.Repository, error)
	GetRepositoryByURL(ctx context.Context, url string) (*models.Repository, error)
	CreateRepository(ctx context.Context, repo *models.Repository) error
	UpdateRepository(ctx context.Context, repo *models.Repository) error
	ListRepositories(ctx context.Context, filter *RepositoryFilter) ([]models.Repository, int64, error)
	DeleteRepository(ctx context.Context, id string) error
	SearchRepositories(ctx context.Context, query string, limit, offset int) ([]models.Repository, error)

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
	ListAnalyses(ctx context.Context, repoID string, limit, offset int) ([]models.CodeAnalysis, int64, error)
	GetLatestAnalysis(ctx context.Context, repoID string, analysisType models.AnalysisType) (*models.CodeAnalysis, error)
	GetRepositoriesNeedingAnalysis(ctx context.Context, limit int) ([]models.Repository, error)
	SumTokensUsedSince(ctx context.Context, since time.Time) (int64, error)

	// Code Embedding operations
	CreateCodeEmbedding(ctx context.Context, embedding *models.CodeEmbedding) error
	SearchEmbeddings(ctx context.Context, repoID string, vector []float32, limit int) ([]models.CodeEmbedding, error)
	DeleteEmbeddingsByRepository(ctx context.Context, repoID string) error

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
	OwnerUserID    string
	Type           models.RepositoryType
	IsPublic       bool
	AnalysisStatus string
	SearchQuery    string
	Limit          int
	Offset         int
}
