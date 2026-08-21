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
	CreatedByUserID *string
	IsPublic        bool
	Metadata        sql.NullString // JSONB → text (cast in SQL); NullString tolerates absent rows
	OwnerTeamID     sql.NullString
	OwnerTeamName   sql.NullString
	OwnerTeamSlug   sql.NullString
	LastSyncedAt    *time.Time
	SyncStatus      string
	SyncError       sql.NullString
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// Coverage from the newest coverage_uploads row (LATERAL join). All
	// nullable — a repository whose CI never uploaded has no row at all.
	TestCoverage       sql.NullFloat64
	TestedLines        sql.NullInt64
	UncoveredLines     sql.NullInt64
	CoverageStatus     sql.NullString
	CoverageUploadedAt sql.NullTime

	// Scorecard signals. EXISTS rather than a LATERAL LIMIT 1 because the
	// question is boolean — Postgres short-circuits a semi-join on the first
	// matching row.
	HasDocs    bool
	HasWebhook bool
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
            r.created_by_user_id,
            r.is_public,
            r.metadata::text                                       AS metadata,
            t.id                                                    AS owner_team_id,
            t.name                                                  AS owner_team_name,
            t.slug                                                  AS owner_team_slug,
            r.last_synced_at,
            r.sync_status,
            r.sync_error,
            r.created_at,
            r.updated_at,
            cov.percentage                                          AS test_coverage,
            cov.lines_covered                                       AS tested_lines,
            (cov.lines_total - cov.lines_covered)                   AS uncovered_lines,
            cov.status                                              AS coverage_status,
            cov.created_at                                          AS coverage_uploaded_at,
            EXISTS (
                SELECT 1 FROM doc_generations dg
                WHERE  dg.repository_id = r.id
                  AND  dg.status = 'completed'
                  AND  dg.deleted_at IS NULL
            )                                                       AS has_docs,
            EXISTS (
                SELECT 1 FROM webhook_configs wc
                WHERE  wc.repository_id = r.id
                  AND  wc.is_active
            )                                                       AS has_webhook
        FROM repositories r
        LEFT JOIN teams t
               ON t.id = r.owner_team_id
              AND t.deleted_at IS NULL
        LEFT JOIN LATERAL (
            SELECT
                cu.percentage,
                cu.lines_covered,
                cu.lines_total,
                cu.status,
                cu.created_at
            FROM   coverage_uploads cu
            WHERE  cu.repository_id = r.id
            ORDER BY cu.created_at DESC
            LIMIT 1
        ) cov ON true
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
		&e.OrganizationID, &e.CreatedByUserID,
		&e.IsPublic, &e.Metadata,
		&e.OwnerTeamID, &e.OwnerTeamName, &e.OwnerTeamSlug,
		&e.LastSyncedAt, &e.SyncStatus, &e.SyncError,
		&e.CreatedAt, &e.UpdatedAt,
		&e.TestCoverage, &e.TestedLines, &e.UncoveredLines, &e.CoverageStatus,
		&e.CoverageUploadedAt,
		&e.HasDocs, &e.HasWebhook,
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

// ResetStaleSyncingRepositories flips any repository stuck in `sync_status='syncing'`
// back to `idle` and returns the affected IDs. Workers do not persist across
// process restarts, so any row left in `syncing` after boot is a stale state
// from a killed process; the caller should re-enqueue a sync task for each
// returned ID.
func (pr *PostgresRepository) ResetStaleSyncingRepositories(ctx context.Context) ([]string, error) {
	const sql = `
        UPDATE repositories
           SET sync_status = 'idle',
               sync_error  = 'reset on startup (previous sync was interrupted)',
               updated_at  = (NOW() AT TIME ZONE 'UTC')
         WHERE sync_status = 'syncing'
           AND deleted_at IS NULL
        RETURNING id`
	var ids []string
	if err := pr.db.WithContext(ctx).Raw(sql).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("reset stale syncing repos: %w", err)
	}
	return ids, nil
}

func (pr *PostgresRepository) UpdateRepository(ctx context.Context, repo *models.Repository) error {
	if !repo.IsValid() {
		return errors.New("invalid repository data")
	}

	// Omit associations: a repository write must only ever touch the
	// repositories row. Belongs-to associations are upserted by GORM before the
	// main row, which is how a partially-populated owner team once got written
	// back with an empty organization_id.
	if err := pr.db.WithContext(ctx).Omit(clause.Associations).Save(repo).Error; err != nil {
		return fmt.Errorf("update repository: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ListRepositories(ctx context.Context, filter *storage.RepositoryFilter) ([]models.Repository, int64, error) {
	// Ownership predicate, shared by the count and the list so the total always
	// matches the rows. A semi-join rather than a JOIN + DISTINCT: DISTINCT would
	// force a sort and break the ordered pagination below. Postgres plans
	// `IN (subquery)` as a hash semi-join.
	ownerClause := ""
	ownerArgs := []any{}
	switch {
	case filter.UnownedOnly:
		ownerClause = " AND r.owner_team_id IS NULL"
	case len(filter.OwnerTeamIDs) > 0:
		ownerClause = " AND r.owner_team_id IN (?)"
		ownerArgs = append(ownerArgs, filter.OwnerTeamIDs)
	}

	// ── 1. Count query (fast, index-only scan) ──────────────────────────────
	countSQL := `
        SELECT COUNT(*)
        FROM   repositories r
        WHERE  r.organization_id = ?
          AND  r.deleted_at IS NULL` + ownerClause

	var total int64
	countArgs := append([]any{filter.OrganizationID}, ownerArgs...)
	if err := pr.db.WithContext(ctx).Raw(countSQL, countArgs...).Scan(&total).Error; err != nil {
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
            r.created_by_user_id,
            r.is_public,
            r.metadata::text                                       AS metadata,
            t.id                                                    AS owner_team_id,
            t.name                                                  AS owner_team_name,
            t.slug                                                  AS owner_team_slug,
            r.last_synced_at,
            r.sync_status,
            r.sync_error,
            r.created_at,
            r.updated_at,
            cov.percentage                                          AS test_coverage,
            cov.lines_covered                                       AS tested_lines,
            (cov.lines_total - cov.lines_covered)                   AS uncovered_lines,
            cov.status                                              AS coverage_status,
            cov.created_at                                          AS coverage_uploaded_at,
            EXISTS (
                SELECT 1 FROM doc_generations dg
                WHERE  dg.repository_id = r.id
                  AND  dg.status = 'completed'
                  AND  dg.deleted_at IS NULL
            )                                                       AS has_docs,
            EXISTS (
                SELECT 1 FROM webhook_configs wc
                WHERE  wc.repository_id = r.id
                  AND  wc.is_active
            )                                                       AS has_webhook
        FROM repositories r
        LEFT JOIN teams t
               ON t.id = r.owner_team_id
              AND t.deleted_at IS NULL
        LEFT JOIN LATERAL (
            SELECT
                cu.percentage,
                cu.lines_covered,
                cu.lines_total,
                cu.status,
                cu.created_at
            FROM   coverage_uploads cu
            WHERE  cu.repository_id = r.id
            ORDER BY cu.created_at DESC
            LIMIT 1
        ) cov ON true
        WHERE r.organization_id = ?
          AND r.deleted_at IS NULL` + ownerClause + `
        ORDER BY r.created_at DESC
        LIMIT  ?
        OFFSET ?`

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	listArgs := append([]any{filter.OrganizationID}, ownerArgs...)
	listArgs = append(listArgs, limit, filter.Offset)
	rows, err := pr.db.WithContext(ctx).Raw(listSQL, listArgs...).Rows()
	if err != nil {
		return nil, 0, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()

	var repos []models.Repository
	for rows.Next() {
		var e enrichedRepo
		if err := rows.Scan(
			&e.ID, &e.Name, &e.Description, &e.URL, &e.Type,
			&e.OrganizationID, &e.CreatedByUserID,
			&e.IsPublic, &e.Metadata,
			&e.OwnerTeamID, &e.OwnerTeamName, &e.OwnerTeamSlug,
			&e.LastSyncedAt, &e.SyncStatus, &e.SyncError,
			&e.CreatedAt, &e.UpdatedAt,
			&e.TestCoverage, &e.TestedLines, &e.UncoveredLines, &e.CoverageStatus,
			&e.CoverageUploadedAt,
			&e.HasDocs, &e.HasWebhook,
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
	if e.Metadata.Valid && e.Metadata.String != "" {
		if err := json.Unmarshal([]byte(e.Metadata.String), &meta); err != nil {
			// Bad data in storage shouldn't crash the list endpoint, but it
			// is loud enough to investigate.
			meta = models.RepositoryMetadata{}
		}
	}

	var createdByUserID string
	if e.CreatedByUserID != nil {
		createdByUserID = *e.CreatedByUserID
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
		CreatedByUserID: createdByUserID,
		IsPublic:        e.IsPublic,
		Metadata:        meta,
		LastSyncedAt:    lastSyncedAt,
		SyncStatus:      e.SyncStatus,
		SyncError:       e.SyncError.String,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}

	if e.OwnerTeamID.Valid {
		repo.OwnerTeamID = &e.OwnerTeamID.String
		repo.OwnerTeam = &models.TeamRef{
			ID:   e.OwnerTeamID.String,
			Name: e.OwnerTeamName.String,
			Slug: e.OwnerTeamSlug.String,
		}
	}

	// Coverage comes straight from the newest CI upload. HasCoverage is false
	// when the LATERAL join found no row, which the DTO uses to distinguish
	// "never measured" from "measured 0%".
	repo.EnrichedStats = &models.EnrichedStats{
		HasDocs:        e.HasDocs,
		HasWebhook:     e.HasWebhook,
		TestCoverage:   e.TestCoverage.Float64,
		TestedLines:    int(e.TestedLines.Int64),
		UncoveredLines: int(e.UncoveredLines.Int64),
		CoverageStatus: e.CoverageStatus.String,
		HasCoverage:    e.TestCoverage.Valid,
	}
	if e.CoverageUploadedAt.Valid {
		t := e.CoverageUploadedAt.Time.UTC().Format(time.RFC3339)
		repo.EnrichedStats.CoverageUploadedAt = &t
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

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

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

// SumTokensUsedSince returns the total tokens used for completed AI work since
// the given time. Documentation generation is the only LLM-backed feature left,
// so `doc_generations` is the whole budget.
func (pr *PostgresRepository) SumTokensUsedSince(ctx context.Context, organizationID string, since time.Time) (int64, error) {
	var total int64
	query := pr.db.WithContext(ctx).
		Model(&models.DocGeneration{}).
		Select("COALESCE(SUM(tokens_used), 0)").
		Where("created_at >= ? AND status = ?", since, models.DocGenerationStatusCompleted)
	if organizationID != "" {
		query = query.Where("organization_id = ?", organizationID)
	}
	if err := query.Scan(&total).Error; err != nil {
		return 0, fmt.Errorf("sum tokens used since: %w", err)
	}
	return total, nil
}
