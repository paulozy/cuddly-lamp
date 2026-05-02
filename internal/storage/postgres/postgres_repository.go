package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
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

// enrichedRepo is a flat struct for scanning the enriched list query result.
// It matches the SELECT column order exactly.
type enrichedRepo struct {
	// Core repository fields
	ID              string
	Name            string
	Description     string
	URL             string
	Type            string
	OrganizationID  string
	OwnerUserID     *string
	CreatedByUserID *string
	IsPublic        bool
	Metadata        []byte // JSONB → raw bytes
	AnalysisStatus  string
	AnalysisError   sql.NullString
	ReviewsCount    int
	LastAnalyzedAt  *time.Time
	LastSyncedAt    *time.Time
	SyncStatus      string
	SyncError       sql.NullString
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// Aggregated stats from LATERAL joins
	TotalAnalyses    int64
	IssueCount       sql.NullInt64
	CriticalCount    sql.NullInt64
	ErrorCount       sql.NullInt64
	WarningCount     sql.NullInt64
	TestCoverage     sql.NullFloat64
	CoverageStatus   sql.NullString
	AvgComplexity    sql.NullFloat64
	LatestAnalyzedAt sql.NullTime
}

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
				"output_language", "updated_at",
			}),
		}).
		Create(cfg).Error; err != nil {
		return fmt.Errorf("upsert organization config: %w", err)
	}
	return nil
}

// ============ Repository Operations ============

func (pr *PostgresRepository) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	// Use the same enriched query as ListRepositories but filter by ID
	listSQL := `
        SELECT
            r.id,
            r.name,
            r.description,
            r.url,
            r.type,
            r.organization_id,
            r.owner_user_id,
            r.created_by_user_id,
            r.is_public,
            r.metadata,
            r.analysis_status,
            r.analysis_error,
            r.reviews_count,
            r.last_analyzed_at,
            r.last_synced_at,
            r.sync_status,
            r.sync_error,
            r.created_at,
            r.updated_at,
            COALESCE(agg.total_analyses, 0)                        AS total_analyses,
            latest.issue_count                                      AS issue_count,
            latest.critical_count                                   AS critical_count,
            latest.error_count                                      AS error_count,
            latest.warning_count                                    AS warning_count,
            (latest.metrics->>'test_coverage')::float               AS test_coverage,
            (latest.metrics->>'coverage_status')                    AS coverage_status,
            (latest.metrics->>'avg_cyclomatic_complexity')::float   AS avg_cyclomatic_complexity,
            latest.created_at                                       AS latest_analyzed_at
        FROM repositories r
        LEFT JOIN LATERAL (
            SELECT COUNT(*) AS total_analyses
            FROM   code_analyses ca
            WHERE  ca.repository_id = r.id
              AND  ca.deleted_at IS NULL
        ) agg ON true
        LEFT JOIN LATERAL (
            SELECT
                ca.issue_count,
                ca.critical_count,
                ca.error_count,
                ca.warning_count,
                ca.metrics,
                ca.created_at
            FROM   code_analyses ca
            WHERE  ca.repository_id = r.id
              AND  ca.type          = 'metrics'
              AND  ca.status        = 'completed'
              AND  ca.deleted_at    IS NULL
            ORDER BY ca.created_at DESC
            LIMIT 1
        ) latest ON true
        WHERE r.id = ?
          AND r.deleted_at IS NULL`

	rows, err := pr.db.WithContext(ctx).Raw(listSQL, id).Rows()
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil // Not found
	}

	var e enrichedRepo
	if err := rows.Scan(
		&e.ID, &e.Name, &e.Description, &e.URL, &e.Type,
		&e.OrganizationID, &e.OwnerUserID, &e.CreatedByUserID,
		&e.IsPublic, &e.Metadata,
		&e.AnalysisStatus, &e.AnalysisError, &e.ReviewsCount,
		&e.LastAnalyzedAt, &e.LastSyncedAt, &e.SyncStatus, &e.SyncError,
		&e.CreatedAt, &e.UpdatedAt,
		&e.TotalAnalyses,
		&e.IssueCount, &e.CriticalCount, &e.ErrorCount, &e.WarningCount,
		&e.TestCoverage, &e.CoverageStatus, &e.AvgComplexity,
		&e.LatestAnalyzedAt,
	); err != nil {
		return nil, fmt.Errorf("scan repository row: %w", err)
	}

	repo := enrichedRepoToModel(e)
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
	// ── 1. Count query (fast, index-only scan) ──────────────────────────────
	countSQL := `
        SELECT COUNT(*)
        FROM   repositories r
        WHERE  r.organization_id = ?
          AND  r.deleted_at IS NULL`

	var total int64
	if err := pr.db.WithContext(ctx).Raw(countSQL, filter.OrganizationID).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count repositories: %w", err)
	}
	if total == 0 {
		return []models.Repository{}, 0, nil
	}

	// ── 2. Enriched list query ───────────────────────────────────────────────
	listSQL := `
        SELECT
            r.id,
            r.name,
            r.description,
            r.url,
            r.type,
            r.organization_id,
            r.owner_user_id,
            r.created_by_user_id,
            r.is_public,
            r.metadata,
            r.analysis_status,
            r.analysis_error,
            r.reviews_count,
            r.last_analyzed_at,
            r.last_synced_at,
            r.sync_status,
            r.sync_error,
            r.created_at,
            r.updated_at,
            COALESCE(agg.total_analyses, 0)                        AS total_analyses,
            latest.issue_count                                      AS issue_count,
            latest.critical_count                                   AS critical_count,
            latest.error_count                                      AS error_count,
            latest.warning_count                                    AS warning_count,
            (latest.metrics->>'test_coverage')::float               AS test_coverage,
            (latest.metrics->>'coverage_status')                    AS coverage_status,
            (latest.metrics->>'avg_cyclomatic_complexity')::float   AS avg_cyclomatic_complexity,
            latest.created_at                                       AS latest_analyzed_at
        FROM repositories r
        LEFT JOIN LATERAL (
            SELECT COUNT(*) AS total_analyses
            FROM   code_analyses ca
            WHERE  ca.repository_id = r.id
              AND  ca.deleted_at IS NULL
        ) agg ON true
        LEFT JOIN LATERAL (
            SELECT
                ca.issue_count,
                ca.critical_count,
                ca.error_count,
                ca.warning_count,
                ca.metrics,
                ca.created_at
            FROM   code_analyses ca
            WHERE  ca.repository_id = r.id
              AND  ca.type          = 'metrics'
              AND  ca.status        = 'completed'
              AND  ca.deleted_at    IS NULL
            ORDER BY ca.created_at DESC
            LIMIT 1
        ) latest ON true
        WHERE r.organization_id = ?
          AND r.deleted_at IS NULL
        ORDER BY r.created_at DESC
        LIMIT  ?
        OFFSET ?`

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := pr.db.WithContext(ctx).Raw(listSQL, filter.OrganizationID, limit, filter.Offset).Rows()
	if err != nil {
		return nil, 0, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()

	var repos []models.Repository
	for rows.Next() {
		var e enrichedRepo
		if err := rows.Scan(
			&e.ID, &e.Name, &e.Description, &e.URL, &e.Type,
			&e.OrganizationID, &e.OwnerUserID, &e.CreatedByUserID,
			&e.IsPublic, &e.Metadata,
			&e.AnalysisStatus, &e.AnalysisError, &e.ReviewsCount,
			&e.LastAnalyzedAt, &e.LastSyncedAt, &e.SyncStatus, &e.SyncError,
			&e.CreatedAt, &e.UpdatedAt,
			&e.TotalAnalyses,
			&e.IssueCount, &e.CriticalCount, &e.ErrorCount, &e.WarningCount,
			&e.TestCoverage, &e.CoverageStatus, &e.AvgComplexity,
			&e.LatestAnalyzedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan repository row: %w", err)
		}
		repos = append(repos, enrichedRepoToModel(e))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate repository rows: %w", err)
	}

	return repos, total, nil
}

// enrichedRepoToModel converts a flat SQL scan result into a models.Repository.
// The Stats field is populated from the lateral-join columns.
func enrichedRepoToModel(e enrichedRepo) models.Repository {
	var meta models.RepositoryMetadata
	if len(e.Metadata) > 0 {
		_ = json.Unmarshal(e.Metadata, &meta) // ignore unmarshal error → empty metadata
	}

	var ownerUserID, createdByUserID string
	if e.OwnerUserID != nil {
		ownerUserID = *e.OwnerUserID
	}
	if e.CreatedByUserID != nil {
		createdByUserID = *e.CreatedByUserID
	}

	var lastAnalyzedAt time.Time
	if e.LastAnalyzedAt != nil {
		lastAnalyzedAt = *e.LastAnalyzedAt
	}
	var lastSyncedAt time.Time
	if e.LastSyncedAt != nil {
		lastSyncedAt = *e.LastSyncedAt
	}

	repo := models.Repository{
		ID:              e.ID,
		Name:            e.Name,
		Description:     e.Description,
		URL:             e.URL,
		Type:            models.RepositoryType(e.Type),
		OrganizationID:  e.OrganizationID,
		OwnerUserID:     ownerUserID,
		CreatedByUserID: createdByUserID,
		IsPublic:        e.IsPublic,
		Metadata:        meta,
		AnalysisStatus:  e.AnalysisStatus,
		AnalysisError:   e.AnalysisError.String,
		ReviewsCount:    e.ReviewsCount,
		LastAnalyzedAt:  lastAnalyzedAt,
		LastSyncedAt:    lastSyncedAt,
		SyncStatus:      e.SyncStatus,
		SyncError:       e.SyncError.String,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}

	// Attach enriched stats so the service layer can compute quality score
	repo.EnrichedStats = &models.EnrichedStats{
		TotalAnalyses:      int(e.TotalAnalyses),
		IssueCount:         int(e.IssueCount.Int64),
		CriticalCount:      int(e.CriticalCount.Int64),
		ErrorCount:         int(e.ErrorCount.Int64),
		WarningCount:       int(e.WarningCount.Int64),
		TestCoverage:       e.TestCoverage.Float64,
		CoverageStatus:     e.CoverageStatus.String,
		AvgComplexity:      e.AvgComplexity.Float64,
		HasMetricsAnalysis: e.IssueCount.Valid, // Valid=true only when LATERAL returned a row
	}
	if e.LatestAnalyzedAt.Valid {
		t := e.LatestAnalyzedAt.Time.UTC().Format(time.RFC3339)
		repo.EnrichedStats.LatestAnalyzedAt = &t
	}

	return repo
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

// ============ Repository Relationship Operations ============

func (pr *PostgresRepository) CreateRepositoryRelationship(ctx context.Context, rel *models.RepositoryRelationship) error {
	if !rel.IsValid() {
		return errors.New("invalid repository relationship data")
	}
	if rel.Metadata == nil {
		rel.Metadata = map[string]interface{}{}
	}
	if err := pr.db.WithContext(ctx).Create(rel).Error; err != nil {
		return fmt.Errorf("create repository relationship: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetRepositoryRelationship(ctx context.Context, id string) (*models.RepositoryRelationship, error) {
	var rel models.RepositoryRelationship
	if err := pr.db.WithContext(ctx).
		Where("deleted_at IS NULL").
		First(&rel, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get repository relationship: %w", err)
	}
	return &rel, nil
}

func (pr *PostgresRepository) UpdateRepositoryRelationship(ctx context.Context, rel *models.RepositoryRelationship) error {
	if !rel.IsValid() {
		return errors.New("invalid repository relationship data")
	}
	if rel.Metadata == nil {
		rel.Metadata = map[string]interface{}{}
	}
	if err := pr.db.WithContext(ctx).Save(rel).Error; err != nil {
		return fmt.Errorf("update repository relationship: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) DeleteRepositoryRelationship(ctx context.Context, id string) error {
	updates := map[string]interface{}{
		"deleted_at": time.Now().UTC(),
		"updated_at": time.Now().UTC(),
	}
	if err := pr.db.WithContext(ctx).
		Model(&models.RepositoryRelationship{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("delete repository relationship: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListRepositoryRelationships(ctx context.Context, filter storage.RepositoryRelationshipFilter) ([]models.RepositoryRelationship, error) {
	var relationships []models.RepositoryRelationship
	query := pr.db.WithContext(ctx).Where("deleted_at IS NULL")
	if filter.OrganizationID != "" {
		query = query.Where("organization_id = ?", filter.OrganizationID)
	}
	if filter.RepositoryID != "" {
		query = query.Where("(source_repository_id = ? OR target_repository_id = ?)", filter.RepositoryID, filter.RepositoryID)
	}
	if filter.Kind != "" {
		query = query.Where("kind = ?", filter.Kind)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if err := query.Order("created_at ASC").Find(&relationships).Error; err != nil {
		return nil, fmt.Errorf("list repository relationships: %w", err)
	}
	return relationships, nil
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

func (pr *PostgresRepository) GetLatestAnalysisForPullRequest(ctx context.Context, repoID string, pullRequestID int, analysisType models.AnalysisType) (*models.CodeAnalysis, error) {
	var analysis models.CodeAnalysis
	if err := pr.db.WithContext(ctx).
		Where("repository_id = ? AND pull_request_id = ? AND type = ?", repoID, pullRequestID, analysisType).
		Order("created_at DESC").
		First(&analysis).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest pull request analysis: %w", err)
	}
	return &analysis, nil
}

func (pr *PostgresRepository) ListLatestAnalysesForPullRequests(ctx context.Context, repoID string, pullRequestIDs []int, analysisType models.AnalysisType) (map[int]models.CodeAnalysis, error) {
	out := make(map[int]models.CodeAnalysis)
	if len(pullRequestIDs) == 0 {
		return out, nil
	}

	var analyses []models.CodeAnalysis
	if err := pr.db.WithContext(ctx).
		Where("repository_id = ? AND pull_request_id IN ? AND type = ?", repoID, pullRequestIDs, analysisType).
		Order("pull_request_id ASC, created_at DESC").
		Find(&analyses).Error; err != nil {
		return nil, fmt.Errorf("list latest pull request analyses: %w", err)
	}

	for _, analysis := range analyses {
		if analysis.PullRequestID == nil {
			continue
		}
		if _, exists := out[*analysis.PullRequestID]; exists {
			continue
		}
		out[*analysis.PullRequestID] = analysis
	}

	return out, nil
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

// ============ Code Template Operations ============

func (pr *PostgresRepository) CreateCodeTemplate(ctx context.Context, template *models.CodeTemplate) error {
	if !template.IsValid() {
		return errors.New("invalid code template data")
	}
	if err := pr.db.WithContext(ctx).Create(template).Error; err != nil {
		return fmt.Errorf("create code template: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetCodeTemplate(ctx context.Context, id string) (*models.CodeTemplate, error) {
	var template models.CodeTemplate
	if err := pr.db.WithContext(ctx).First(&template, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get code template: %w", err)
	}
	return &template, nil
}

func (pr *PostgresRepository) UpdateCodeTemplate(ctx context.Context, template *models.CodeTemplate) error {
	if !template.IsValid() {
		return errors.New("invalid code template data")
	}
	if err := pr.db.WithContext(ctx).Save(template).Error; err != nil {
		return fmt.Errorf("update code template: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListCodeTemplates(ctx context.Context, filter storage.CodeTemplateFilter) ([]models.CodeTemplate, int64, error) {
	var templates []models.CodeTemplate
	var total int64

	query := pr.db.WithContext(ctx).Model(&models.CodeTemplate{})
	if filter.OrganizationID != "" {
		query = query.Where("organization_id = ?", filter.OrganizationID)
	}
	if filter.RepositoryID != "" {
		query = query.Where("repository_id = ?", filter.RepositoryID)
	}
	if filter.IsPinned != nil {
		query = query.Where("is_pinned = ?", *filter.IsPinned)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count code templates: %w", err)
	}
	if err := query.
		Limit(filter.Limit).
		Offset(filter.Offset).
		Order("created_at DESC").
		Find(&templates).Error; err != nil {
		return nil, 0, fmt.Errorf("list code templates: %w", err)
	}
	return templates, total, nil
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

// SumTokensUsedSince returns the total tokens used for completed AI work since the given time.
func (pr *PostgresRepository) SumTokensUsedSince(ctx context.Context, organizationID string, since time.Time) (int64, error) {
	var total int64
	query := `
		SELECT COALESCE(SUM(tokens_used), 0)
		FROM (
			SELECT ca.tokens_used
			FROM code_analyses ca
			JOIN repositories r ON r.id = ca.repository_id
			WHERE ca.created_at >= ? AND ca.status = 'completed'
	`
	if organizationID != "" {
		query += " AND r.organization_id = ?"
	}
	query += `
			UNION ALL
			SELECT ct.tokens_used
			FROM code_templates ct
			WHERE ct.created_at >= ? AND ct.status = 'completed'
	`
	if organizationID != "" {
		query += " AND ct.organization_id = ?"
	}
	query += ") AS combined"

	args := []any{since}
	if organizationID != "" {
		args = append(args, organizationID)
	}
	args = append(args, since)
	if organizationID != "" {
		args = append(args, organizationID)
	}

	if err := pr.db.WithContext(ctx).Raw(query, args...).Scan(&total).Error; err != nil {
		return 0, fmt.Errorf("sum tokens used since: %w", err)
	}
	return total, nil
}
