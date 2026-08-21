package services

import (
	"context"
	"errors"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// mockAuthzRepo answers only the team-membership question the predicate asks.
type mockAuthzRepo struct {
	storage.Repository
	teamsByUser map[string][]string
	lookupErr   error
}

func (m *mockAuthzRepo) ListTeamIDsForUser(_ context.Context, _, userID string) ([]string, error) {
	if m.lookupErr != nil {
		return nil, m.lookupErr
	}
	return m.teamsByUser[userID], nil
}

func teamOwned(teamID string) *models.Repository {
	id := teamID
	return &models.Repository{ID: "repo-1", OrganizationID: "org-1", OwnerTeamID: &id}
}

func unowned() *models.Repository {
	return &models.Repository{ID: "repo-1", OrganizationID: "org-1"}
}

func authzService(teams map[string][]string) *RepositoryService {
	return NewRepositoryService(&mockAuthzRepo{teamsByUser: teams}, nil, nil)
}

// Invariant 1: ownership never denies what the role already allows.
func TestCanWriteRepository_MaintainerAndAdminBypassOwnership(t *testing.T) {
	svc := authzService(map[string][]string{}) // nobody is on any team

	for _, role := range []models.UserRole{models.RoleMaintainer, models.RoleAdmin} {
		if !svc.CanWriteRepository(context.Background(), role, "u1", teamOwned("team-a")) {
			t.Errorf("%s should write a repository owned by a team they are not on", role)
		}
		if !svc.CanWriteRepository(context.Background(), role, "u1", unowned()) {
			t.Errorf("%s should write an unowned repository", role)
		}
	}
}

// Invariant 2: ownership never grants what the role lacks.
func TestCanWriteRepository_ViewerOnOwningTeamStillCannotWrite(t *testing.T) {
	svc := authzService(map[string][]string{"u1": {"team-a"}})

	if svc.CanWriteRepository(context.Background(), models.RoleViewer, "u1", teamOwned("team-a")) {
		t.Error("a viewer on the owning team must still be read-only")
	}
}

// The one role ownership actually moves.
func TestCanWriteRepository_DeveloperWritesOnlyTheirTeamsRepositories(t *testing.T) {
	svc := authzService(map[string][]string{"u1": {"team-a", "team-b"}})
	ctx := context.Background()

	if !svc.CanWriteRepository(ctx, models.RoleDeveloper, "u1", teamOwned("team-a")) {
		t.Error("developer should write a repository owned by their team")
	}
	if !svc.CanWriteRepository(ctx, models.RoleDeveloper, "u1", teamOwned("team-b")) {
		t.Error("developer should write a repository owned by any of their teams")
	}
	if svc.CanWriteRepository(ctx, models.RoleDeveloper, "u1", teamOwned("team-z")) {
		t.Error("developer must not write a repository owned by another team")
	}
	if svc.CanWriteRepository(ctx, models.RoleDeveloper, "u2", teamOwned("team-a")) {
		t.Error("a developer on no team must not write")
	}
}

// Claiming an unowned repository should be a deliberate act, not something any
// developer inherits because nobody got there first.
func TestCanWriteRepository_DeveloperCannotWriteUnowned(t *testing.T) {
	svc := authzService(map[string][]string{"u1": {"team-a"}})

	if svc.CanWriteRepository(context.Background(), models.RoleDeveloper, "u1", unowned()) {
		t.Error("an unowned repository must not be writable by a developer")
	}
}

// A failure resolving membership must read as "no", never as "yes".
func TestCanWriteRepository_FailsClosedOnLookupError(t *testing.T) {
	svc := NewRepositoryService(&mockAuthzRepo{lookupErr: errors.New("db down")}, nil, nil)

	if svc.CanWriteRepository(context.Background(), models.RoleDeveloper, "u1", teamOwned("team-a")) {
		t.Error("a membership lookup failure must not grant write access")
	}
}

// An unrecognised role sits below viewer in the hierarchy and must not write.
func TestCanWriteRepository_RejectsUnknownRole(t *testing.T) {
	svc := authzService(map[string][]string{"u1": {"team-a"}})

	if svc.CanWriteRepository(context.Background(), models.UserRole("root"), "u1", teamOwned("team-a")) {
		t.Error("an unknown role must not be treated as privileged")
	}
}
