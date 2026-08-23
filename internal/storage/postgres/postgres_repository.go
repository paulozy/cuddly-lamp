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
			// This list must name exactly the columns that exist on
			// organization_configs — Postgres rejects the whole statement for a
			// single unknown name, and it named five columns that migration 023
			// dropped with the AI features, which made every config update fail.
			// Adding a field to OrganizationConfig means adding it here, or the
			// value silently fails to persist on an existing row.
			DoUpdates: clause.AssignmentColumns([]string{
				"anthropic_api_key", "anthropic_tokens_per_hour",
				"github_token", "gitlab_token", "gitlab_base_url",
				"webhook_base_url",
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

// ============ Derived Architecture Operations ============

// UpsertRepositoryFact writes one extractor's output for one repository,
// keyed by (repository_id, fact_kind) as migration 032 declares.
func (pr *PostgresRepository) UpsertRepositoryFact(ctx context.Context, fact *models.RepositoryFact) error {
	if fact.OrganizationID == "" || fact.RepositoryID == "" || !models.IsValidRepositoryFactKind(fact.FactKind) {
		return errors.New("invalid repository fact data")
	}
	if len(fact.Payload) == 0 {
		fact.Payload = []byte("{}")
	}
	now := time.Now().UTC()
	if fact.ExtractedAt.IsZero() {
		fact.ExtractedAt = now
	}
	if err := pr.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "repository_id"}, {Name: "fact_kind"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"organization_id", "payload", "tree_sha", "complete",
				"extractor_version", "extracted_at", "updated_at",
			}),
		}).
		Create(fact).Error; err != nil {
		return fmt.Errorf("upsert repository fact: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetRepositoryFact(ctx context.Context, repositoryID string, kind models.RepositoryFactKind) (*models.RepositoryFact, error) {
	var fact models.RepositoryFact
	if err := pr.db.WithContext(ctx).
		First(&fact, "repository_id = ? AND fact_kind = ?", repositoryID, kind).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get repository fact: %w", err)
	}
	return &fact, nil
}

func (pr *PostgresRepository) ListRepositoryFacts(ctx context.Context, organizationID string, kind models.RepositoryFactKind) ([]models.RepositoryFact, error) {
	var facts []models.RepositoryFact
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND fact_kind = ?", organizationID, kind).
		Order("repository_id ASC").
		Find(&facts).Error; err != nil {
		return nil, fmt.Errorf("list repository facts: %w", err)
	}
	return facts, nil
}

// UpsertDerivedRelationship writes a derived edge idempotently.
//
// It is a lookup-then-write rather than a bare ON CONFLICT for one reason: the
// unique index is partial on `deleted_at IS NULL`, so a live row can collide
// with an identical soft-deleted twin, and the right answer there is to *revive*
// the twin, not insert a second row. Reviving keeps the id stable, which keeps
// every deep link to the edge — and anything attached to it — alive. A new row
// would silently destroy both.
func (pr *PostgresRepository) UpsertDerivedRelationship(ctx context.Context, rel *models.RepositoryRelationship) error {
	if !rel.IsValid() {
		return errors.New("invalid repository relationship data")
	}
	if !rel.IsDerived() {
		return errors.New("derived relationship requires a derivation key")
	}
	if rel.Metadata == nil {
		rel.Metadata = map[string]interface{}{}
	}
	now := time.Now().UTC()
	if rel.LastSeenAt == nil {
		rel.LastSeenAt = &now
	}

	return pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.RepositoryRelationship
		err := tx.
			Where(`organization_id = ? AND source_repository_id = ? AND target_repository_id = ?
			       AND kind = ? AND derivation_key = ? AND derivation_fingerprint = ?`,
				rel.OrganizationID, rel.SourceRepositoryID, rel.TargetRepositoryID,
				rel.Kind, rel.DerivationKey, rel.DerivationFingerprint).
			// A live row wins over a soft-deleted twin; among twins, the newest.
			Order("deleted_at IS NOT NULL ASC, created_at DESC").
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// created_by_user_id is a uuid column and nobody created this row, so
			// it has to be omitted rather than sent as the Go zero value: GORM
			// would write '' and Postgres rejects that as a uuid. Same
			// NULL-is-not-empty-string trap the derivation columns avoid by
			// being pointers.
			insert := tx
			if rel.CreatedByUserID == "" {
				insert = tx.Omit("created_by_user_id")
			}
			if createErr := insert.Create(rel).Error; createErr != nil {
				return fmt.Errorf("insert derived relationship: %w", createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("look up derived relationship: %w", err)
		}

		updates := map[string]interface{}{
			"label":        rel.Label,
			"description":  rel.Description,
			"source":       rel.Source,
			"confidence":   rel.Confidence,
			"metadata":     rel.Metadata,
			"last_seen_at": rel.LastSeenAt,
			"updated_at":   now,
			"deleted_at":   nil,
		}
		if updateErr := tx.Model(&models.RepositoryRelationship{}).
			Where("id = ?", existing.ID).
			Updates(updates).Error; updateErr != nil {
			return fmt.Errorf("revive derived relationship: %w", updateErr)
		}
		rel.ID = existing.ID
		rel.CreatedAt = existing.CreatedAt
		rel.DeletedAt = nil
		return nil
	})
}

// SweepDerivedRelationships retires the rows this run did not re-observe.
//
// The `derivation_key = ?` predicate is the whole safety story: a human row
// carries NULL there and can never match, so no amount of deriver confusion can
// delete a declaration.
func (pr *PostgresRepository) SweepDerivedRelationships(ctx context.Context, derivationKey string, runStartedAt time.Time) (int64, error) {
	if derivationKey == "" {
		return 0, errors.New("sweep requires a derivation key")
	}
	now := time.Now().UTC()
	result := pr.db.WithContext(ctx).
		Model(&models.RepositoryRelationship{}).
		Where("derivation_key = ? AND last_seen_at < ? AND deleted_at IS NULL", derivationKey, runStartedAt.UTC()).
		Updates(map[string]interface{}{"deleted_at": now, "updated_at": now})
	if result.Error != nil {
		return 0, fmt.Errorf("sweep derived relationships: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (pr *PostgresRepository) ListSuppressions(ctx context.Context, organizationID, derivationKey string) ([]models.DerivationSuppression, error) {
	var suppressions []models.DerivationSuppression
	query := pr.db.WithContext(ctx).Where("organization_id = ?", organizationID)
	if derivationKey != "" {
		query = query.Where("derivation_key = ?", derivationKey)
	}
	if err := query.Find(&suppressions).Error; err != nil {
		return nil, fmt.Errorf("list derivation suppressions: %w", err)
	}
	return suppressions, nil
}

func (pr *PostgresRepository) CreateSuppression(ctx context.Context, suppression *models.DerivationSuppression) error {
	if suppression.OrganizationID == "" || suppression.DerivationKey == "" || suppression.DerivationFingerprint == "" {
		return errors.New("invalid derivation suppression data")
	}
	if err := pr.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "organization_id"}, {Name: "derivation_key"}, {Name: "derivation_fingerprint"},
			},
			DoNothing: true,
		}).
		Create(suppression).Error; err != nil {
		return fmt.Errorf("create derivation suppression: %w", err)
	}
	return nil
}

// ============ API Discovery Operations ============

// UpsertDerivedAPI inserts a discovered API, or revives and refreshes the row
// already at that spec path.
//
// Same lookup-then-write shape as UpsertDerivedRelationship, and for the same
// reason: the unique index is partial on `deleted_at IS NULL`, so a live row can
// collide with an identical soft-deleted twin, and reviving keeps the id stable.
//
// Note what the update does *not* touch: spec_path. That is the identity, so a
// spec that moved is a different row — a new API plus a sweep of the old one,
// which is exactly what the catalog should say.
func (pr *PostgresRepository) UpsertDerivedAPI(ctx context.Context, api *models.API) error {
	if !api.IsValid() {
		return errors.New("invalid api data")
	}
	if !api.IsDerived() {
		return errors.New("derived api requires a derivation key")
	}
	if api.Metadata == nil {
		api.Metadata = map[string]interface{}{}
	}
	now := time.Now().UTC()
	if api.LastSeenAt == nil {
		api.LastSeenAt = &now
	}

	return pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.API
		err := tx.
			Where("repository_id = ? AND spec_path = ?", api.RepositoryID, api.SpecPath).
			Order("deleted_at IS NOT NULL ASC, created_at DESC").
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := tx.Create(api).Error; createErr != nil {
				return fmt.Errorf("insert derived api: %w", createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("look up derived api: %w", err)
		}

		updates := map[string]interface{}{
			"kind":                   api.Kind,
			"title":                  api.Title,
			"version":                api.Version,
			"operation_count":        api.OperationCount,
			"derivation_key":         api.DerivationKey,
			"derivation_fingerprint": api.DerivationFingerprint,
			"metadata":               api.Metadata,
			"last_seen_at":           api.LastSeenAt,
			"updated_at":             now,
			"deleted_at":             nil,
		}
		if updateErr := tx.Model(&models.API{}).
			Where("id = ?", existing.ID).
			Updates(updates).Error; updateErr != nil {
			return fmt.Errorf("revive derived api: %w", updateErr)
		}
		api.ID = existing.ID
		api.CreatedAt = existing.CreatedAt
		api.DeletedAt = nil
		return nil
	})
}

func (pr *PostgresRepository) SweepDerivedAPIs(ctx context.Context, repositoryID, derivationKey string, runStartedAt time.Time) (int64, error) {
	if repositoryID == "" || derivationKey == "" {
		return 0, errors.New("sweep requires a repository and a derivation key")
	}
	now := time.Now().UTC()
	result := pr.db.WithContext(ctx).
		Model(&models.API{}).
		Where("repository_id = ? AND derivation_key = ? AND last_seen_at < ? AND deleted_at IS NULL",
			repositoryID, derivationKey, runStartedAt.UTC()).
		Updates(map[string]interface{}{"deleted_at": now, "updated_at": now})
	if result.Error != nil {
		return 0, fmt.Errorf("sweep derived apis: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (pr *PostgresRepository) ListAPIs(ctx context.Context, organizationID string) ([]models.API, error) {
	var apis []models.API
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND deleted_at IS NULL", organizationID).
		Order("repository_id ASC, spec_path ASC").
		Find(&apis).Error; err != nil {
		return nil, fmt.Errorf("list apis: %w", err)
	}
	return apis, nil
}

// ============ Resource Discovery Operations ============

// UpsertDerivedResource inserts a resource or finds the one that already carries
// its locator, and writes the id back onto the argument.
//
// The lookup is by *identity*, not by derivation key, and that is the whole point
// of the shared case: two repositories that independently name
// `postgres://db.prod.internal:5432/orders` must converge on one row. The scoped
// case keys on the repository as well, so two repositories each running a local
// Postgres stay two rows — see migration 035 for why that is the desired answer
// rather than a limitation.
func (pr *PostgresRepository) UpsertDerivedResource(ctx context.Context, resource *models.Resource) error {
	if !resource.IsValid() {
		return errors.New("invalid resource data")
	}
	if !resource.IsDerived() {
		return errors.New("derived resource requires a derivation key")
	}
	if resource.Metadata == nil {
		resource.Metadata = map[string]interface{}{}
	}
	now := time.Now().UTC()
	if resource.LastSeenAt == nil {
		resource.LastSeenAt = &now
	}

	return pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("organization_id = ? AND engine = ?", resource.OrganizationID, resource.Engine)
		if resource.IsScoped() {
			query = query.Where("scoped_repository_id = ? AND COALESCE(display_name, '') = ?",
				*resource.ScopedRepositoryID, resource.DisplayName)
		} else {
			// COALESCE mirrors the unique index: Postgres treats NULLs as distinct
			// in a unique index, so a NULL port would defeat the unification the
			// index exists to provide.
			query = query.Where(`scoped_repository_id IS NULL AND host = ?
			                     AND COALESCE(port, -1) = ? AND COALESCE(namespace, '') = ?`,
				resource.Host, coalescePort(resource.Port), resource.Namespace)
		}

		var existing models.Resource
		err := query.Order("deleted_at IS NOT NULL ASC, created_at DESC").First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := tx.Create(resource).Error; createErr != nil {
				return fmt.Errorf("insert derived resource: %w", createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("look up derived resource: %w", err)
		}

		updates := map[string]interface{}{
			"display_name": resource.DisplayName,
			"metadata":     resource.Metadata,
			"last_seen_at": resource.LastSeenAt,
			"updated_at":   now,
			"deleted_at":   nil,
		}
		if updateErr := tx.Model(&models.Resource{}).
			Where("id = ?", existing.ID).
			Updates(updates).Error; updateErr != nil {
			return fmt.Errorf("revive derived resource: %w", updateErr)
		}
		resource.ID = existing.ID
		resource.CreatedAt = existing.CreatedAt
		resource.DeletedAt = nil
		return nil
	})
}

func coalescePort(port *int) int {
	if port == nil {
		return -1
	}
	return *port
}

// UpsertDerivedRepositoryResource records one repository's claim to use a
// resource.
func (pr *PostgresRepository) UpsertDerivedRepositoryResource(ctx context.Context, link *models.RepositoryResource) error {
	if !link.IsValid() {
		return errors.New("invalid repository resource data")
	}
	if !link.IsDerived() {
		return errors.New("derived repository resource requires a derivation key")
	}
	if link.Metadata == nil {
		link.Metadata = map[string]interface{}{}
	}
	now := time.Now().UTC()
	if link.LastSeenAt == nil {
		link.LastSeenAt = &now
	}

	return pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.RepositoryResource
		err := tx.
			Where(`repository_id = ? AND resource_id = ?
			       AND derivation_key = ? AND derivation_fingerprint = ?`,
				link.RepositoryID, link.ResourceID, link.DerivationKey, link.DerivationFingerprint).
			Order("deleted_at IS NOT NULL ASC, created_at DESC").
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := tx.Create(link).Error; createErr != nil {
				return fmt.Errorf("insert repository resource: %w", createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("look up repository resource: %w", err)
		}

		updates := map[string]interface{}{
			"confidence":   link.Confidence,
			"metadata":     link.Metadata,
			"last_seen_at": link.LastSeenAt,
			"updated_at":   now,
			"deleted_at":   nil,
		}
		if updateErr := tx.Model(&models.RepositoryResource{}).
			Where("id = ?", existing.ID).
			Updates(updates).Error; updateErr != nil {
			return fmt.Errorf("revive repository resource: %w", updateErr)
		}
		link.ID = existing.ID
		link.CreatedAt = existing.CreatedAt
		link.DeletedAt = nil
		return nil
	})
}

func (pr *PostgresRepository) SweepDerivedRepositoryResources(ctx context.Context, repositoryID, derivationKey string, runStartedAt time.Time) (int64, error) {
	if repositoryID == "" || derivationKey == "" {
		return 0, errors.New("sweep requires a repository and a derivation key")
	}
	now := time.Now().UTC()
	result := pr.db.WithContext(ctx).
		Model(&models.RepositoryResource{}).
		Where("repository_id = ? AND derivation_key = ? AND last_seen_at < ? AND deleted_at IS NULL",
			repositoryID, derivationKey, runStartedAt.UTC()).
		Updates(map[string]interface{}{"deleted_at": now, "updated_at": now})
	if result.Error != nil {
		return 0, fmt.Errorf("sweep repository resources: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// RetireOrphanResources soft-deletes derived resources nothing references.
//
// It runs after every repository's join has been reconciled, because a shared
// resource is only orphaned once the *last* repository stops naming it — deciding
// that from inside one repository's sweep is impossible. Human-created rows are
// excluded by `derivation_key IS NOT NULL`, the same structural guarantee the
// relationship sweep relies on.
func (pr *PostgresRepository) RetireOrphanResources(ctx context.Context, organizationID string) (int64, error) {
	if organizationID == "" {
		return 0, errors.New("retire requires an organization id")
	}
	now := time.Now().UTC()
	result := pr.db.WithContext(ctx).
		Model(&models.Resource{}).
		Where(`organization_id = ? AND derivation_key IS NOT NULL AND deleted_at IS NULL
		       AND NOT EXISTS (
		           SELECT 1 FROM repository_resources rr
		            WHERE rr.resource_id = resources.id AND rr.deleted_at IS NULL
		       )`, organizationID).
		Updates(map[string]interface{}{"deleted_at": now, "updated_at": now})
	if result.Error != nil {
		return 0, fmt.Errorf("retire orphan resources: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (pr *PostgresRepository) ListResources(ctx context.Context, organizationID string) ([]models.Resource, error) {
	var resources []models.Resource
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND deleted_at IS NULL", organizationID).
		Order("engine ASC, display_name ASC").
		Find(&resources).Error; err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	return resources, nil
}

func (pr *PostgresRepository) ListRepositoryResources(ctx context.Context, organizationID string) ([]models.RepositoryResource, error) {
	var links []models.RepositoryResource
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND deleted_at IS NULL", organizationID).
		Order("repository_id ASC").
		Find(&links).Error; err != nil {
		return nil, fmt.Errorf("list repository resources: %w", err)
	}
	return links, nil
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

func (pr *PostgresRepository) GetOAuthConnectionByUser(ctx context.Context, userID, provider string) (*models.OAuthConnection, error) {
	var conn models.OAuthConnection
	if err := pr.db.WithContext(ctx).
		Where("user_id = ? AND provider = ?", userID, provider).
		First(&conn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Not every member connected every provider; the caller reports
			// "cannot confirm yet" rather than failing a check.
			return nil, nil
		}
		return nil, fmt.Errorf("get oauth connection by user: %w", err)
	}
	return &conn, nil
}

func (pr *PostgresRepository) UpsertOAuthConnection(ctx context.Context, conn *models.OAuthConnection) error {
	if err := pr.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "provider"}, {Name: "provider_user_id"}},
			// provider_username belongs here or it would never persist for a
			// connection that already exists — which is every returning user,
			// and the whole point of backfilling it on login.
			DoUpdates: clause.AssignmentColumns([]string{"user_id", "provider_username", "access_token", "updated_at"}),
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
