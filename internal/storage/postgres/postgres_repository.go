package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/pgvector/pgvector-go"
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

// ============ Organization Operations ============

func (pr *PostgresRepository) GetOrganization(ctx context.Context, id string) (*models.Organization, error) {
	var org models.Organization
	if err := pr.db.WithContext(ctx).First(&org, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return &org, nil
}

func (pr *PostgresRepository) GetOrganizationBySlug(ctx context.Context, slug string) (*models.Organization, error) {
	var org models.Organization
	if err := pr.db.WithContext(ctx).First(&org, "slug = ?", slug).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get organization by slug: %w", err)
	}
	return &org, nil
}

func (pr *PostgresRepository) CreateOrganization(ctx context.Context, org *models.Organization) error {
	if !org.IsValid() {
		return errors.New("invalid organization data")
	}
	if err := pr.db.WithContext(ctx).Create(org).Error; err != nil {
		return fmt.Errorf("create organization: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetOrganizationMember(ctx context.Context, orgID, userID string) (*models.OrganizationMember, error) {
	var member models.OrganizationMember
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ? AND is_active = true", orgID, userID).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get organization member: %w", err)
	}
	return &member, nil
}

func (pr *PostgresRepository) ListOrganizationMembersForUser(ctx context.Context, userID string) ([]models.OrganizationMember, error) {
	var members []models.OrganizationMember
	if err := pr.db.WithContext(ctx).
		Preload("Organization").
		Joins("JOIN organizations ON organizations.id = organization_members.organization_id").
		Where("organization_members.user_id = ? AND organization_members.is_active = true AND organizations.is_active = true", userID).
		Order("organization_members.created_at ASC").
		Find(&members).Error; err != nil {
		return nil, fmt.Errorf("list organization members for user: %w", err)
	}
	return members, nil
}

func (pr *PostgresRepository) CreateOrganizationMember(ctx context.Context, member *models.OrganizationMember) error {
	if err := pr.db.WithContext(ctx).Create(member).Error; err != nil {
		return fmt.Errorf("create organization member: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) CountOrganizationMembers(ctx context.Context, orgID string) (int64, error) {
	var total int64
	if err := pr.db.WithContext(ctx).
		Model(&models.OrganizationMember{}).
		Where("organization_id = ?", orgID).
		Count(&total).Error; err != nil {
		return 0, fmt.Errorf("count organization members: %w", err)
	}
	return total, nil
}

func (pr *PostgresRepository) GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
	var cfg models.OrganizationConfig
	if err := pr.db.WithContext(ctx).First(&cfg, "organization_id = ?", orgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get organization config: %w", err)
	}
	cfg.ApplyDefaults()
	return &cfg, nil
}

func (pr *PostgresRepository) UpsertOrganizationConfig(ctx context.Context, cfg *models.OrganizationConfig) error {
	cfg.ApplyDefaults()
	if err := pr.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "organization_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"anthropic_api_key", "anthropic_tokens_per_hour", "github_token",
				"github_pr_review_enabled", "webhook_base_url", "embeddings_provider",
				"voyage_api_key", "embeddings_model", "embeddings_dimensions",
				"github_client_id", "github_client_secret", "github_callback_url",
				"gitlab_client_id", "gitlab_client_secret", "gitlab_callback_url",
				"updated_at",
			}),
		}).
		Create(cfg).Error; err != nil {
		return fmt.Errorf("upsert organization config: %w", err)
	}
	return nil
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

func (pr *PostgresRepository) GetRepositoryByURL(ctx context.Context, organizationID, url string) (*models.Repository, error) {
	var repo models.Repository
	query := pr.db.WithContext(ctx).Where("url = ?", url)
	if organizationID != "" {
		query = query.Where("organization_id = ?", organizationID)
	}
	if err := query.First(&repo).Error; err != nil {
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
	if filter.OrganizationID != "" {
		query = query.Where("organization_id = ?", filter.OrganizationID)
	}
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

// ============ WebhookConfig Operations ============

func (pr *PostgresRepository) GetWebhookConfigByRepoID(ctx context.Context, repoID string) (*models.WebhookConfig, error) {
	var cfg models.WebhookConfig
	if err := pr.db.WithContext(ctx).First(&cfg, "repository_id = ?", repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get webhook config: %w", err)
	}
	return &cfg, nil
}

func (pr *PostgresRepository) CreateWebhookConfig(ctx context.Context, cfg *models.WebhookConfig) error {
	if err := pr.db.WithContext(ctx).Create(cfg).Error; err != nil {
		return fmt.Errorf("create webhook config: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) UpdateWebhookConfig(ctx context.Context, cfg *models.WebhookConfig) error {
	if err := pr.db.WithContext(ctx).Save(cfg).Error; err != nil {
		return fmt.Errorf("update webhook config: %w", err)
	}
	return nil
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

func (pr *PostgresRepository) GetAnalysesByRepository(ctx context.Context, repoID string, limit, offset int) ([]models.CodeAnalysis, int64, error) {
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

func (pr *PostgresRepository) ListAnalyses(ctx context.Context, repoID string, limit, offset int) ([]models.CodeAnalysis, int64, error) {
	return pr.GetAnalysesByRepository(ctx, repoID, limit, offset)
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

// ============ Package Dependency Operations ============

func (pr *PostgresRepository) UpsertPackageDependency(ctx context.Context, dep *models.PackageDependency) error {
	if dep.RepositoryID == "" || dep.Name == "" || dep.Ecosystem == "" {
		return errors.New("invalid package dependency data")
	}
	if err := pr.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "repository_id"}, {Name: "name"}, {Name: "ecosystem"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"current_version", "latest_version", "manifest_file", "is_direct_dependency",
				"is_vulnerable", "vulnerability_cves", "update_available", "last_scanned_at", "updated_at",
			}),
		}).
		Create(dep).Error; err != nil {
		return fmt.Errorf("upsert package dependency: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListPackageDependencies(ctx context.Context, repoID string, onlyVulnerable bool) ([]*models.PackageDependency, error) {
	var deps []*models.PackageDependency
	query := pr.db.WithContext(ctx).Where("repository_id = ?", repoID)
	if onlyVulnerable {
		query = query.Where("is_vulnerable = true")
	}
	if err := query.Order("ecosystem ASC, name ASC").Find(&deps).Error; err != nil {
		return nil, fmt.Errorf("list package dependencies: %w", err)
	}
	return deps, nil
}

func (pr *PostgresRepository) UpdatePackageDependencyVulnStatus(ctx context.Context, id string, isVulnerable bool, cves []string, latestVersion string) error {
	updates := map[string]interface{}{
		"is_vulnerable":      isVulnerable,
		"vulnerability_cves": models.StringArray(cves),
		"latest_version":     latestVersion,
		"update_available":   latestVersion != "",
		"updated_at":         time.Now().UTC(),
	}
	if err := pr.db.WithContext(ctx).
		Model(&models.PackageDependency{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("update package dependency vulnerability status: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) DeletePackageDependencies(ctx context.Context, repoID string) error {
	if err := pr.db.WithContext(ctx).Where("repository_id = ?", repoID).Delete(&models.PackageDependency{}).Error; err != nil {
		return fmt.Errorf("delete package dependencies: %w", err)
	}
	return nil
}

// ============ Code Embedding Operations ============

func (pr *PostgresRepository) CreateCodeEmbedding(ctx context.Context, embedding *models.CodeEmbedding) error {
	if err := pr.db.WithContext(ctx).Create(embedding).Error; err != nil {
		return fmt.Errorf("create code embedding: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) CreateCodeEmbeddings(ctx context.Context, embeddings []models.CodeEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}
	if err := pr.db.WithContext(ctx).CreateInBatches(embeddings, 100).Error; err != nil {
		return fmt.Errorf("create code embeddings: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) SearchEmbeddings(ctx context.Context, filter storage.EmbeddingSearchFilter) ([]models.CodeEmbedding, error) {
	var embeddings []models.CodeEmbedding
	if filter.Limit <= 0 || filter.Limit > 50 {
		filter.Limit = 10
	}
	if filter.MinScore < 0 || filter.MinScore > 1 {
		filter.MinScore = 0.55
	}

	queryVector := pgvector.NewVector(filter.Vector)
	where := []string{"repository_id = ?"}
	args := []interface{}{filter.RepositoryID}
	if filter.Provider != "" {
		where = append(where, "provider = ?")
		args = append(args, filter.Provider)
	}
	if filter.Model != "" {
		where = append(where, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.Dimension > 0 {
		where = append(where, "dimension = ?")
		args = append(args, filter.Dimension)
	}
	if filter.Branch != "" {
		where = append(where, "branch = ?")
		args = append(args, filter.Branch)
	}

	searchText := strings.TrimSpace(filter.Query)
	searchPattern := "%" + escapeLike(searchText) + "%"
	candidateLimit := filter.Limit * 5
	if candidateLimit < 50 {
		candidateLimit = 50
	}

	sql := fmt.Sprintf(`
		SELECT *
		FROM (
			SELECT
				code_embeddings.*,
				LEAST(
					1.0,
					(1 - (embedding <=> ?)) +
					CASE WHEN ? <> '' AND content ILIKE ? ESCAPE '\' THEN 0.15 ELSE 0 END +
					CASE WHEN ? <> '' AND file_path ILIKE ? ESCAPE '\' THEN 0.10 ELSE 0 END +
					CASE WHEN ? <> '' AND language ILIKE ? ESCAPE '\' THEN 0.05 ELSE 0 END
				) AS score
			FROM code_embeddings
			WHERE %s
			ORDER BY embedding <=> ?
			LIMIT ?
		) ranked
		WHERE score >= ?
		ORDER BY score DESC, file_path ASC, start_line ASC
		LIMIT ?
	`, strings.Join(where, " AND "))

	queryArgs := []interface{}{
		queryVector,
		searchText, searchPattern,
		searchText, searchPattern,
		searchText, searchPattern,
	}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, queryVector, candidateLimit, filter.MinScore, filter.Limit)

	if err := pr.db.WithContext(ctx).Raw(sql, queryArgs...).Scan(&embeddings).Error; err != nil {
		return nil, fmt.Errorf("search embeddings: %w", err)
	}

	return embeddings, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func (pr *PostgresRepository) DeleteEmbeddings(ctx context.Context, filter storage.EmbeddingDeleteFilter) error {
	query := pr.db.WithContext(ctx).Where("repository_id = ?", filter.RepositoryID)
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}
	if filter.Model != "" {
		query = query.Where("model = ?", filter.Model)
	}
	if filter.Dimension > 0 {
		query = query.Where("dimension = ?", filter.Dimension)
	}
	if filter.Branch != "" {
		query = query.Where("branch = ?", filter.Branch)
	}
	if err := query.Delete(&models.CodeEmbedding{}).Error; err != nil {
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

// SumTokensUsedSince returns the total tokens used for completed analyses since the given time
func (pr *PostgresRepository) SumTokensUsedSince(ctx context.Context, organizationID string, since time.Time) (int64, error) {
	var total int64
	query := pr.db.WithContext(ctx).
		Model(&models.CodeAnalysis{}).
		Joins("JOIN repositories ON repositories.id = code_analyses.repository_id").
		Where("code_analyses.created_at >= ? AND code_analyses.status = ?", since, "completed")
	if organizationID != "" {
		query = query.Where("repositories.organization_id = ?", organizationID)
	}
	if err := query.
		Select("COALESCE(SUM(tokens_used), 0)").
		Scan(&total).Error; err != nil {
		return 0, fmt.Errorf("sum tokens used since: %w", err)
	}
	return total, nil
}
