package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

var (
	ErrTeamNotFound          = errors.New("team not found")
	ErrTeamSlugTaken         = errors.New("a team with this name already exists in the organization")
	ErrTeamInvalid           = errors.New("team name is required")
	ErrTeamNotLocal          = errors.New("membership of an imported team is managed by its provider")
	ErrUserNotInOrganization = errors.New("that user does not belong to this organization")
)

var teamSlugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

type TeamService struct {
	repo storage.Repository
}

func NewTeamService(repo storage.Repository) *TeamService {
	return &TeamService{repo: repo}
}

func (s *TeamService) CreateTeam(ctx context.Context, orgID, createdByUserID string, req models.CreateTeamRequest) (*models.Team, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrTeamInvalid
	}
	slug := slugifyTeam(req.Slug)
	if slug == "" {
		slug = slugifyTeam(name)
	}
	if slug == "" {
		return nil, ErrTeamInvalid
	}

	existing, err := s.repo.GetTeamBySlug(ctx, orgID, slug)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrTeamSlugTaken
	}

	team := &models.Team{
		OrganizationID: orgID,
		Name:           name,
		Slug:           slug,
		Description:    strings.TrimSpace(req.Description),
	}
	if createdByUserID != "" {
		team.CreatedByUserID = &createdByUserID
	}
	if err := s.repo.CreateTeam(ctx, team); err != nil {
		return nil, err
	}

	// The creator joins as lead. A team nobody is on cannot be contacted, which
	// defeats the point of recording ownership.
	if createdByUserID != "" {
		member := &models.TeamMember{TeamID: team.ID, UserID: createdByUserID, Role: models.TeamRoleLead}
		if err := s.repo.UpsertTeamMember(ctx, member); err != nil {
			utils.Warn("team: failed to add creator as lead", "team_id", team.ID, "error", err)
		}
		team.MemberCount = 1
	}
	return team, nil
}

// ListTeams returns the organization's teams, marking the ones viewerUserID
// belongs to.
//
// The membership lookup is one query for the whole list rather than one per
// team. A failure to resolve it is not fatal: the teams themselves are still
// worth returning, and every team simply reports `viewer_is_member: false` —
// which the UI reads as "no team filter available", not as "you are on no
// teams".
func (s *TeamService) ListTeams(ctx context.Context, orgID, viewerUserID string) ([]models.Team, error) {
	teams, err := s.repo.ListTeams(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if viewerUserID == "" {
		return teams, nil
	}

	mine, err := s.repo.ListTeamIDsForUser(ctx, orgID, viewerUserID)
	if err != nil {
		utils.Warn("team: could not resolve the caller's memberships",
			"user_id", viewerUserID, "error", err)
		return teams, nil
	}
	member := make(map[string]bool, len(mine))
	for _, id := range mine {
		member[id] = true
	}
	for i := range teams {
		teams[i].ViewerIsMember = member[teams[i].ID]
	}
	return teams, nil
}

// getScoped fetches a team and refuses to hand back one belonging to another
// organization, so a guessed id cannot cross the tenant boundary.
func (s *TeamService) getScoped(ctx context.Context, orgID, teamID string) (*models.Team, error) {
	team, err := s.repo.GetTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if team == nil || team.OrganizationID != orgID {
		return nil, ErrTeamNotFound
	}
	return team, nil
}

func (s *TeamService) GetTeam(ctx context.Context, orgID, teamID string) (*models.Team, error) {
	return s.getScoped(ctx, orgID, teamID)
}

func (s *TeamService) UpdateTeam(ctx context.Context, orgID, teamID string, req models.UpdateTeamRequest) (*models.Team, error) {
	team, err := s.getScoped(ctx, orgID, teamID)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ErrTeamInvalid
		}
		team.Name = name
	}
	if req.Description != nil {
		team.Description = strings.TrimSpace(*req.Description)
	}
	// The slug is intentionally immutable: it is the stable handle other things
	// may come to reference.
	if err := s.repo.UpdateTeam(ctx, team); err != nil {
		return nil, err
	}
	return team, nil
}

func (s *TeamService) DeleteTeam(ctx context.Context, orgID, teamID string) error {
	if _, err := s.getScoped(ctx, orgID, teamID); err != nil {
		return err
	}
	if err := s.repo.DeleteTeam(ctx, teamID); err != nil {
		return ErrTeamNotFound
	}
	return nil
}

// ── members ──────────────────────────────────────────────────────────────────

func (s *TeamService) ListMembers(ctx context.Context, orgID, teamID string) ([]models.TeamMemberResponse, error) {
	if _, err := s.getScoped(ctx, orgID, teamID); err != nil {
		return nil, err
	}
	members, err := s.repo.ListTeamMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]models.TeamMemberResponse, 0, len(members))
	for i := range members {
		m := &members[i]
		out = append(out, models.TeamMemberResponse{
			UserID:   m.UserID,
			Email:    m.User.Email,
			FullName: m.User.FullName,
			Role:     m.Role,
		})
	}
	return out, nil
}

func (s *TeamService) AddMember(ctx context.Context, orgID, teamID string, req models.AddTeamMemberRequest) error {
	team, err := s.getScoped(ctx, orgID, teamID)
	if err != nil {
		return err
	}
	// Imported teams mirror the provider. Accepting a local edit would have the
	// next sync silently revert it, which is worse than refusing.
	if team.Source() != models.TeamSourceLocal {
		return ErrTeamNotLocal
	}

	// You can only put people on a team who are in the organization — otherwise
	// team membership becomes a side door into the tenant.
	member, err := s.repo.GetOrganizationMember(ctx, orgID, req.UserID)
	if err != nil {
		return err
	}
	if member == nil {
		return ErrUserNotInOrganization
	}

	role := req.Role
	if role != models.TeamRoleLead {
		role = models.TeamRoleMember
	}
	return s.repo.UpsertTeamMember(ctx, &models.TeamMember{
		TeamID:    teamID,
		UserID:    req.UserID,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	})
}

func (s *TeamService) RemoveMember(ctx context.Context, orgID, teamID, userID string) error {
	team, err := s.getScoped(ctx, orgID, teamID)
	if err != nil {
		return err
	}
	if team.Source() != models.TeamSourceLocal {
		return ErrTeamNotLocal
	}
	if err := s.repo.DeleteTeamMember(ctx, teamID, userID); err != nil {
		return ErrTeamNotFound
	}
	return nil
}

// ── repository ownership ─────────────────────────────────────────────────────

// SetRepositoryOwner assigns or clears a repository's owning team. Passing nil
// clears it; "unowned" is a state the catalog reports, not an error.
func (s *TeamService) SetRepositoryOwner(ctx context.Context, orgID, repoID string, teamID *string) error {
	repo, err := s.repo.GetRepository(ctx, repoID)
	if err != nil {
		return err
	}
	if repo == nil || repo.OrganizationID != orgID {
		return ErrRepositoryNotFound
	}
	if teamID != nil {
		if _, err := s.getScoped(ctx, orgID, *teamID); err != nil {
			return err
		}
	}
	if err := s.repo.SetRepositoryOwnerTeam(ctx, repoID, teamID); err != nil {
		return fmt.Errorf("set repository owner: %w", err)
	}
	return nil
}

func slugifyTeam(raw string) string {
	slug := strings.ToLower(strings.TrimSpace(raw))
	slug = teamSlugInvalid.ReplaceAllString(slug, "-")
	return strings.Trim(slug, "-")
}
