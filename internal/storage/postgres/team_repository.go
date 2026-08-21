package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/gorm"
)

func (pr *PostgresRepository) CreateTeam(ctx context.Context, team *models.Team) error {
	if err := pr.db.WithContext(ctx).Create(team).Error; err != nil {
		return fmt.Errorf("create team: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetTeam(ctx context.Context, id string) (*models.Team, error) {
	var team models.Team
	if err := pr.db.WithContext(ctx).First(&team, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get team: %w", err)
	}
	return &team, nil
}

func (pr *PostgresRepository) GetTeamBySlug(ctx context.Context, orgID, slug string) (*models.Team, error) {
	var team models.Team
	err := pr.db.WithContext(ctx).
		First(&team, "organization_id = ? AND slug = ? AND deleted_at IS NULL", orgID, slug).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get team by slug: %w", err)
	}
	return &team, nil
}

// teamCounts carries the aggregates the team list renders, fetched in one query
// each rather than per-team.
type teamCounts struct {
	TeamID string
	Count  int
}

func (pr *PostgresRepository) ListTeams(ctx context.Context, orgID string) ([]models.Team, error) {
	var teams []models.Team
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND deleted_at IS NULL", orgID).
		Order("name ASC").
		Find(&teams).Error; err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	if len(teams) == 0 {
		return teams, nil
	}

	ids := make([]string, 0, len(teams))
	for i := range teams {
		ids = append(ids, teams[i].ID)
	}

	var memberCounts []teamCounts
	if err := pr.db.WithContext(ctx).
		Table("team_members").
		Select("team_id, COUNT(*) AS count").
		Where("team_id IN ?", ids).
		Group("team_id").
		Scan(&memberCounts).Error; err != nil {
		return nil, fmt.Errorf("count team members: %w", err)
	}

	var repoCounts []teamCounts
	if err := pr.db.WithContext(ctx).
		Table("repositories").
		Select("owner_team_id AS team_id, COUNT(*) AS count").
		Where("owner_team_id IN ? AND deleted_at IS NULL", ids).
		Group("owner_team_id").
		Scan(&repoCounts).Error; err != nil {
		return nil, fmt.Errorf("count team repositories: %w", err)
	}

	members := map[string]int{}
	for _, c := range memberCounts {
		members[c.TeamID] = c.Count
	}
	repos := map[string]int{}
	for _, c := range repoCounts {
		repos[c.TeamID] = c.Count
	}
	for i := range teams {
		teams[i].MemberCount = members[teams[i].ID]
		teams[i].RepositoryCount = repos[teams[i].ID]
	}
	return teams, nil
}

func (pr *PostgresRepository) UpdateTeam(ctx context.Context, team *models.Team) error {
	team.UpdatedAt = time.Now().UTC()
	if err := pr.db.WithContext(ctx).Save(team).Error; err != nil {
		return fmt.Errorf("update team: %w", err)
	}
	return nil
}

// DeleteTeam soft-deletes the team and releases its repositories.
//
// The FK is ON DELETE SET NULL, but a soft delete is an UPDATE — the constraint
// never fires. Without clearing owner_team_id here, every repository the team
// owned would keep pointing at a row that no longer resolves, and the catalog
// would render an owner that cannot be opened.
func (pr *PostgresRepository) DeleteTeam(ctx context.Context, id string) error {
	return pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		res := tx.Model(&models.Team{}).
			Where("id = ? AND deleted_at IS NULL", id).
			Updates(map[string]any{"deleted_at": now, "updated_at": now})
		if res.Error != nil {
			return fmt.Errorf("soft delete team: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&models.Repository{}).
			Where("owner_team_id = ?", id).
			Update("owner_team_id", nil).Error; err != nil {
			return fmt.Errorf("release team repositories: %w", err)
		}
		return nil
	})
}

func (pr *PostgresRepository) ListTeamMembers(ctx context.Context, teamID string) ([]models.TeamMember, error) {
	var members []models.TeamMember
	if err := pr.db.WithContext(ctx).
		Preload("User").
		Where("team_id = ?", teamID).
		Order("created_at ASC").
		Find(&members).Error; err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	return members, nil
}

// ListTeamIDsForUser answers "which teams am I on", which drives both the
// catalog filter and the write-permission check. Scoped to the organization so
// a user's teams in one org never leak into another.
func (pr *PostgresRepository) ListTeamIDsForUser(ctx context.Context, orgID, userID string) ([]string, error) {
	var ids []string
	if err := pr.db.WithContext(ctx).
		Table("team_members tm").
		Joins("JOIN teams t ON t.id = tm.team_id AND t.deleted_at IS NULL").
		Where("tm.user_id = ? AND t.organization_id = ?", userID, orgID).
		Pluck("tm.team_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list team ids for user: %w", err)
	}
	return ids, nil
}

func (pr *PostgresRepository) UpsertTeamMember(ctx context.Context, member *models.TeamMember) error {
	now := time.Now().UTC()
	member.UpdatedAt = now
	if member.CreatedAt.IsZero() {
		member.CreatedAt = now
	}
	err := pr.db.WithContext(ctx).
		Exec(`INSERT INTO team_members (team_id, user_id, role, created_at, updated_at)
		      VALUES (?, ?, ?, ?, ?)
		      ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at`,
			member.TeamID, member.UserID, member.Role, member.CreatedAt, member.UpdatedAt).Error
	if err != nil {
		return fmt.Errorf("upsert team member: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) DeleteTeamMember(ctx context.Context, teamID, userID string) error {
	res := pr.db.WithContext(ctx).
		Where("team_id = ? AND user_id = ?", teamID, userID).
		Delete(&models.TeamMember{})
	if res.Error != nil {
		return fmt.Errorf("delete team member: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SetRepositoryOwnerTeam assigns or clears the owning team. A nil teamID clears
// it, which is a legitimate state rather than an error.
func (pr *PostgresRepository) SetRepositoryOwnerTeam(ctx context.Context, repoID string, teamID *string) error {
	res := pr.db.WithContext(ctx).
		Model(&models.Repository{}).
		Where("id = ? AND deleted_at IS NULL", repoID).
		Updates(map[string]any{"owner_team_id": teamID, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return fmt.Errorf("set repository owner team: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
