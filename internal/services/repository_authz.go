package services

import (
	"context"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// CanWriteRepository is the third authorization layer, on top of the role gate
// in middleware and the organization-scope check in the service layer.
//
// It exists so a team can look after its own services without making everyone a
// maintainer. Two invariants keep it from becoming a competing permission
// system, and both matter:
//
//   - Ownership never DENIES what the role already allows. Maintainer and admin
//     write anything in their organization, owned by them or not.
//   - Ownership never GRANTS what the role lacks. A viewer who happens to sit on
//     the owning team is still a viewer.
//
// So the only role ownership actually moves is `developer`: it may write the
// repositories its teams own, and nothing else.
//
// Membership is direct only — no roll-up through a parent team. Transitive
// ownership is the biggest source of "why can that person edit this", and
// Backstage excludes it from `ownershipEntityRefs` by default for the same
// reason.
func (s *RepositoryService) CanWriteRepository(ctx context.Context, orgRole models.UserRole, userID string, repo *models.Repository) bool {
	// Maintainer and above: role alone decides, ownership is irrelevant.
	if utils.HasPermission(orgRole, models.RoleMaintainer) {
		return true
	}
	// Below developer, ownership grants nothing.
	if !utils.HasPermission(orgRole, models.RoleDeveloper) {
		return false
	}
	// A developer needs the repository to be owned by one of their teams. An
	// unowned repository is not writable by developers — claiming it is a
	// deliberate act, not a side effect of nobody having claimed it.
	if repo.OwnerTeamID == nil || *repo.OwnerTeamID == "" {
		return false
	}
	teamIDs, err := s.repo.ListTeamIDsForUser(ctx, repo.OrganizationID, userID)
	if err != nil {
		// Fail closed: an error looking up membership must not read as permission.
		utils.Warn("authz: failed to resolve team membership", "user_id", userID, "error", err)
		return false
	}
	for _, id := range teamIDs {
		if id == *repo.OwnerTeamID {
			return true
		}
	}
	return false
}
