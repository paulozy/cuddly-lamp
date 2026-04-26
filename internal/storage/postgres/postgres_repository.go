package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PostgresRepository implements RepositoryStorage using GORM
type PostgresRepository struct {
	db *gorm.DB
}

// NewPostgresRepository creates a new PostgreSQL repository
func NewPostgresRepository(db *gorm.DB) storage.Repository {
	return &PostgresRepository{db: db}
}

// ============ User Operations ============

func (pr *PostgresRepository) GetUser(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	if err := pr.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &user, nil
}

func (pr *PostgresRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	if err := pr.db.WithContext(ctx).First(&user, "email = ?", email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &user, nil
}

func (pr *PostgresRepository) CreateUser(ctx context.Context, user *models.User) error {
	if !user.IsValid() {
		return errors.New("invalid user data")
	}

	if err := pr.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) UpdateUser(ctx context.Context, user *models.User) error {
	if !user.IsValid() {
		return errors.New("invalid user data")
	}

	if err := pr.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListUsers(ctx context.Context, limit, offset int) ([]models.User, error) {
	var users []models.User
	if err := pr.db.WithContext(ctx).
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// ============ Repository Operations ============

func (pr *PostgresRepository) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	var repo models.Repository
	if err := pr.db.WithContext(ctx).
		Preload("Members").
		Preload("Webhooks").
		First(&repo, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get repository: %w", err)
	}
	return &repo, nil
}

func (pr *PostgresRepository) GetRepositoryByURL(ctx context.Context, url string) (*models.Repository, error) {
	var repo models.Repository
	if err := pr.db.WithContext(ctx).First(&repo, "url = ?", url).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get repository by url: %w", err)
	}
	return &repo, nil
}

func (pr *PostgresRepository) CreateRepository(ctx context.Context, repo *models.Repository) error {
	if !repo.IsValid() {
		return errors.New("invalid repository data")
	}

	if err := pr.db.WithContext(ctx).Create(repo).Error; err != nil {
		return fmt.Errorf("create repository: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) UpdateRepository(ctx context.Context, repo *models.Repository) error {
	if !repo.IsValid() {
		return errors.New("invalid repository data")
	}

	if err := pr.db.WithContext(ctx).Save(repo).Error; err != nil {
		return fmt.Errorf("update repository: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListRepositories(ctx context.Context, filter *storage.RepositoryFilter) ([]models.Repository, int64, error) {
	var repos []models.Repository
	var total int64

	query := pr.db.WithContext(ctx)

	// Apply filters
	if filter.OwnerUserID != "" {
		query = query.Where("owner_user_id = ?", filter.OwnerUserID)
	}
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.AnalysisStatus != "" {
		query = query.Where("analysis_status = ?", filter.AnalysisStatus)
	}

	// Count total
	if err := query.Model(&models.Repository{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count repositories: %w", err)
	}

	// Apply pagination and fetch
	if err := query.
		Limit(filter.Limit).
		Offset(filter.Offset).
		Order("created_at DESC").
		Find(&repos).Error; err != nil {
		return nil, 0, fmt.Errorf("list repositories: %w", err)
	}

	return repos, total, nil
}

func (pr *PostgresRepository) DeleteRepository(ctx context.Context, id string) error {
	if err := pr.db.WithContext(ctx).Delete(&models.Repository{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) SearchRepositories(ctx context.Context, query string, limit, offset int) ([]models.Repository, error) {
	var repos []models.Repository
	if err := pr.db.WithContext(ctx).
		Where("name ILIKE ? OR description ILIKE ?", "%"+query+"%", "%"+query+"%").
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("search repositories: %w", err)
	}
	return repos, nil
}

// ============ Webhook Operations ============

func (pr *PostgresRepository) GetWebhook(ctx context.Context, id string) (*models.Webhook, error) {
	var webhook models.Webhook
	if err := pr.db.WithContext(ctx).First(&webhook, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get webhook: %w", err)
	}
	return &webhook, nil
}

func (pr *PostgresRepository) GetWebhookByDeliveryID(ctx context.Context, deliveryID string) (*models.Webhook, error) {
	var webhook models.Webhook
	if err := pr.db.WithContext(ctx).First(&webhook, "delivery_id = ?", deliveryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get webhook by delivery id: %w", err)
	}
	return &webhook, nil
}

func (pr *PostgresRepository) CreateWebhook(ctx context.Context, webhook *models.Webhook) error {
	if !webhook.IsValid() {
		return errors.New("invalid webhook data")
	}

	if err := pr.db.WithContext(ctx).Create(webhook).Error; err != nil {
		return fmt.Errorf("create webhook: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) UpdateWebhook(ctx context.Context, webhook *models.Webhook) error {
	if !webhook.IsValid() {
		return errors.New("invalid webhook data")
	}

	if err := pr.db.WithContext(ctx).Save(webhook).Error; err != nil {
		return fmt.Errorf("update webhook: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListPendingWebhooks(ctx context.Context, limit int) ([]models.Webhook, error) {
	var webhooks []models.Webhook
	if err := pr.db.WithContext(ctx).
		Where("status = ?", "pending").
		Limit(limit).
		Order("created_at ASC").
		Find(&webhooks).Error; err != nil {
		return nil, fmt.Errorf("list pending webhooks: %w", err)
	}
	return webhooks, nil
}

func (pr *PostgresRepository) ListFailedWebhooks(ctx context.Context, limit, offset int) ([]models.Webhook, error) {
	var webhooks []models.Webhook
	if err := pr.db.WithContext(ctx).
		Where("status = ? AND next_retry_at <= ?", "failed", time.Now()).
		Limit(limit).
		Offset(offset).
		Order("created_at ASC").
		Find(&webhooks).Error; err != nil {
		return nil, fmt.Errorf("list failed webhooks: %w", err)
	}
	return webhooks, nil
}

// ============ Code Analysis Operations ============

func (pr *PostgresRepository) GetCodeAnalysis(ctx context.Context, id string) (*models.CodeAnalysis, error) {
	var analysis models.CodeAnalysis
	if err := pr.db.WithContext(ctx).First(&analysis, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get code analysis: %w", err)
	}
	return &analysis, nil
}

func (pr *PostgresRepository) CreateCodeAnalysis(ctx context.Context, analysis *models.CodeAnalysis) error {
	if !analysis.IsValid() {
		return errors.New("invalid analysis data")
	}

	if err := pr.db.WithContext(ctx).Create(analysis).Error; err != nil {
		return fmt.Errorf("create code analysis: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) UpdateCodeAnalysis(ctx context.Context, analysis *models.CodeAnalysis) error {
	if !analysis.IsValid() {
		return errors.New("invalid analysis data")
	}

	if err := pr.db.WithContext(ctx).Save(analysis).Error; err != nil {
		return fmt.Errorf("update code analysis: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListAnalyses(ctx context.Context, repoID string, limit, offset int) ([]models.CodeAnalysis, int64, error) {
	var analyses []models.CodeAnalysis
	var total int64

	query := pr.db.WithContext(ctx).Where("repository_id = ?", repoID)

	if err := query.Model(&models.CodeAnalysis{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count analyses: %w", err)
	}

	if err := query.
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&analyses).Error; err != nil {
		return nil, 0, fmt.Errorf("list analyses: %w", err)
	}

	return analyses, total, nil
}

func (pr *PostgresRepository) GetLatestAnalysis(ctx context.Context, repoID string, analysisType models.AnalysisType) (*models.CodeAnalysis, error) {
	var analysis models.CodeAnalysis
	if err := pr.db.WithContext(ctx).
		Where("repository_id = ? AND type = ?", repoID, analysisType).
		Order("created_at DESC").
		First(&analysis).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest analysis: %w", err)
	}
	return &analysis, nil
}

func (pr *PostgresRepository) GetRepositoriesNeedingAnalysis(ctx context.Context, limit int) ([]models.Repository, error) {
	var repos []models.Repository

	// Repositories that haven't been analyzed or were analyzed more than 24 hours ago
	cutoffTime := time.Now().Add(-24 * time.Hour)

	if err := pr.db.WithContext(ctx).
		Where("analysis_status != ? AND (last_analyzed_at IS NULL OR last_analyzed_at < ?)", "in_progress", cutoffTime).
		Limit(limit).
		Order("last_analyzed_at ASC").
		Find(&repos).Error; err != nil {
		return nil, fmt.Errorf("get repositories needing analysis: %w", err)
	}

	return repos, nil
}

// ============ Code Embedding Operations ============

func (pr *PostgresRepository) CreateCodeEmbedding(ctx context.Context, embedding *models.CodeEmbedding) error {
	if err := pr.db.WithContext(ctx).Create(embedding).Error; err != nil {
		return fmt.Errorf("create code embedding: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) SearchEmbeddings(ctx context.Context, repoID string, vector []float32, limit int) ([]models.CodeEmbedding, error) {
	var embeddings []models.CodeEmbedding

	// Use PostgreSQL pgvector similarity search
	if err := pr.db.WithContext(ctx).
		Where("repository_id = ?", repoID).
		Order(clause.Expr{SQL: "embedding <-> ?", Vars: []interface{}{vector}}).
		Limit(limit).
		Find(&embeddings).Error; err != nil {
		return nil, fmt.Errorf("search embeddings: %w", err)
	}

	return embeddings, nil
}

func (pr *PostgresRepository) DeleteEmbeddingsByRepository(ctx context.Context, repoID string) error {
	if err := pr.db.WithContext(ctx).
		Delete(&models.CodeEmbedding{}, "repository_id = ?", repoID).Error; err != nil {
		return fmt.Errorf("delete embeddings by repository: %w", err)
	}
	return nil
}

// ============ Token Operations ============

func (pr *PostgresRepository) CreateToken(ctx context.Context, token *models.Token) error {
	if err := pr.db.WithContext(ctx).Create(token).Error; err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetTokenByJTI(ctx context.Context, jti string) (*models.Token, error) {
	var token models.Token
	if err := pr.db.WithContext(ctx).First(&token, "jti = ?", jti).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get token by jti: %w", err)
	}
	return &token, nil
}

func (pr *PostgresRepository) GetTokenByHash(ctx context.Context, tokenHash string) (*models.Token, error) {
	var token models.Token
	if err := pr.db.WithContext(ctx).First(&token, "token_hash = ?", tokenHash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get token by hash: %w", err)
	}
	return &token, nil
}

func (pr *PostgresRepository) RevokeTokenFamily(ctx context.Context, familyID uuid.UUID, reason string) error {
	now := time.Now().UTC()
	return pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&models.Token{}).
			Where("family_id = ? AND is_revoked = false", familyID).
			Updates(map[string]interface{}{
				"is_revoked":    true,
				"revoked_at":    now,
				"revoke_reason": reason,
			}).Error
	})
}

func (pr *PostgresRepository) RevokeToken(ctx context.Context, jti string, reason string) error {
	if err := pr.db.WithContext(ctx).
		Model(&models.Token{}).
		Where("jti = ?", jti).
		Updates(map[string]interface{}{
			"is_revoked":    true,
			"revoked_at":    time.Now(),
			"revoke_reason": reason,
		}).Error; err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) UpdateTokenLastUsed(ctx context.Context, jti string) error {
	if err := pr.db.WithContext(ctx).
		Model(&models.Token{}).
		Where("jti = ?", jti).
		Update("last_used_at", time.Now()).Error; err != nil {
		return fmt.Errorf("update token last used: %w", err)
	}

	return nil
}

// ============ OAuth Operations ============

func (pr *PostgresRepository) GetOAuthConnection(ctx context.Context, provider, providerUserID string) (*models.OAuthConnection, error) {
	var conn models.OAuthConnection
	if err := pr.db.WithContext(ctx).
		Where("provider = ? AND provider_user_id = ?", provider, providerUserID).
		First(&conn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get oauth connection: %w", err)
	}
	return &conn, nil
}

func (pr *PostgresRepository) UpsertOAuthConnection(ctx context.Context, conn *models.OAuthConnection) error {
	if err := pr.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "provider"}, {Name: "provider_user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"user_id", "access_token", "updated_at"}),
		}).
		Create(conn).Error; err != nil {
		return fmt.Errorf("upsert oauth connection: %w", err)
	}
	return nil
}
