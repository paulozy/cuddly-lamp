package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"gorm.io/gorm"
)

// ── mock ─────────────────────────────────────────────────────────────────────

type mockMembershipRepo struct {
	storage.Repository
	orgs    map[string]*models.Organization
	users   map[string]*models.User
	members map[string]*models.OrganizationMember // key: orgID:userID
	invites map[string]*models.OrganizationInvite // key: id
}

func newMockMembershipRepo() *mockMembershipRepo {
	return &mockMembershipRepo{
		orgs:    map[string]*models.Organization{},
		users:   map[string]*models.User{},
		members: map[string]*models.OrganizationMember{},
		invites: map[string]*models.OrganizationInvite{},
	}
}

func (m *mockMembershipRepo) ListOrganizationMembers(_ context.Context, orgID string) ([]models.OrganizationMember, error) {
	var out []models.OrganizationMember
	for _, mem := range m.members {
		if mem.OrganizationID != orgID {
			continue
		}
		cp := *mem
		if u, ok := m.users[mem.UserID]; ok {
			cp.User = *u
		}
		out = append(out, cp)
	}
	return out, nil
}

func (m *mockMembershipRepo) CountOrganizationAdmins(_ context.Context, orgID string) (int64, error) {
	var n int64
	for _, mem := range m.members {
		if mem.OrganizationID == orgID && mem.Role == models.RoleAdmin && mem.IsActive {
			n++
		}
	}
	return n, nil
}

func (m *mockMembershipRepo) GetOrganizationMember(_ context.Context, orgID, userID string) (*models.OrganizationMember, error) {
	mem, ok := m.members[orgID+":"+userID]
	if !ok {
		return nil, nil
	}
	return mem, nil
}

func (m *mockMembershipRepo) UpdateOrganizationMemberRole(_ context.Context, orgID, userID string, role models.UserRole) error {
	m.members[orgID+":"+userID].Role = role
	return nil
}

func (m *mockMembershipRepo) DeleteOrganizationMember(_ context.Context, orgID, userID string) error {
	delete(m.members, orgID+":"+userID)
	return nil
}

func (m *mockMembershipRepo) CreateOrganizationInvite(_ context.Context, inv *models.OrganizationInvite) error {
	if inv.ID == "" {
		inv.ID = uuid.New().String()
	}
	inv.CreatedAt = time.Now().UTC()
	m.invites[inv.ID] = inv
	return nil
}

func (m *mockMembershipRepo) GetOrganizationInviteByHash(_ context.Context, hash string) (*models.OrganizationInvite, error) {
	for _, inv := range m.invites {
		if inv.TokenHash == hash {
			return inv, nil
		}
	}
	return nil, nil
}

func (m *mockMembershipRepo) GetOrganizationInvite(_ context.Context, id string) (*models.OrganizationInvite, error) {
	inv, ok := m.invites[id]
	if !ok {
		return nil, nil
	}
	return inv, nil
}

func (m *mockMembershipRepo) ListOrganizationInvites(_ context.Context, orgID string) ([]models.OrganizationInvite, error) {
	var out []models.OrganizationInvite
	for _, inv := range m.invites {
		if inv.OrganizationID == orgID {
			out = append(out, *inv)
		}
	}
	return out, nil
}

func (m *mockMembershipRepo) RevokeOrganizationInvite(_ context.Context, id string) error {
	inv, ok := m.invites[id]
	if !ok || inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return gorm.ErrRecordNotFound
	}
	now := time.Now().UTC()
	inv.RevokedAt = &now
	return nil
}

func (m *mockMembershipRepo) AcceptOrganizationInvite(_ context.Context, inviteID string, member *models.OrganizationMember) error {
	inv, ok := m.invites[inviteID]
	if !ok || !inv.IsRedeemable(time.Now().UTC()) {
		return gorm.ErrRecordNotFound
	}
	now := time.Now().UTC()
	inv.AcceptedAt = &now
	inv.AcceptedByUserID = &member.UserID
	m.members[member.OrganizationID+":"+member.UserID] = member
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func seedOrgWithAdmin(repo *mockMembershipRepo) (orgID, adminID string) {
	orgID = uuid.New().String()
	adminID = uuid.New().String()
	repo.orgs[orgID] = &models.Organization{ID: orgID, Name: "Acme", Slug: "acme", IsActive: true}
	repo.users[adminID] = &models.User{ID: adminID, Email: "admin@acme.com", FullName: "Admin", IsActive: true}
	repo.members[orgID+":"+adminID] = &models.OrganizationMember{
		OrganizationID: orgID, UserID: adminID, Role: models.RoleAdmin, IsActive: true,
	}
	return orgID, adminID
}

// ── invites ──────────────────────────────────────────────────────────────────

func TestCreateInvite_ReturnsPlaintextOnceAndStoresOnlyHash(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	invite, token, err := svc.CreateInvite(context.Background(), orgID, adminID,
		models.CreateInviteRequest{Email: "New@Acme.com", Role: models.RoleMaintainer})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	if !strings.HasPrefix(token, "inv_") {
		t.Errorf("token = %q, want inv_ prefix", token)
	}
	if invite.TokenHash == token {
		t.Fatal("the plaintext token was stored instead of its hash")
	}
	if invite.TokenHash != HashInviteToken(token) {
		t.Error("stored hash does not match the issued token")
	}
	// Addresses are normalised so a re-invite with different casing collides.
	if invite.Email != "new@acme.com" {
		t.Errorf("email = %q, want lowercased", invite.Email)
	}
	if invite.Role != models.RoleMaintainer {
		t.Errorf("role = %q, want maintainer", invite.Role)
	}
}

func TestCreateInvite_RejectsInvalidInput(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	if _, _, err := svc.CreateInvite(context.Background(), orgID, adminID,
		models.CreateInviteRequest{Email: "not-an-email"}); err != ErrInviteInvalidEmail {
		t.Errorf("err = %v, want ErrInviteInvalidEmail", err)
	}
	if _, _, err := svc.CreateInvite(context.Background(), orgID, adminID,
		models.CreateInviteRequest{Email: "x@acme.com", Role: models.UserRole("root")}); err != ErrInviteInvalidRole {
		t.Errorf("err = %v, want ErrInviteInvalidRole", err)
	}
	if _, _, err := svc.CreateInvite(context.Background(), orgID, adminID,
		models.CreateInviteRequest{Email: "admin@acme.com"}); err != ErrInviteAlreadyMember {
		t.Errorf("err = %v, want ErrInviteAlreadyMember", err)
	}
}

func TestResolveInvite_HappyPath(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	_, token, _ := svc.CreateInvite(context.Background(), orgID, adminID,
		models.CreateInviteRequest{Email: "new@acme.com"})

	got, err := svc.ResolveInvite(context.Background(), token, "new@acme.com")
	if err != nil {
		t.Fatalf("ResolveInvite: %v", err)
	}
	if got.OrganizationID != orgID {
		t.Errorf("organization = %q, want %q", got.OrganizationID, orgID)
	}
}

// The invite is bound to an address; otherwise a forwarded link is a skeleton
// key to the organization.
func TestResolveInvite_RejectsDifferentEmail(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	_, token, _ := svc.CreateInvite(context.Background(), orgID, adminID,
		models.CreateInviteRequest{Email: "invited@acme.com"})

	if _, err := svc.ResolveInvite(context.Background(), token, "someone.else@evil.com"); err != ErrInviteNotFound {
		t.Errorf("err = %v, want ErrInviteNotFound", err)
	}
	// Case and surrounding whitespace must not defeat the match, though.
	if _, err := svc.ResolveInvite(context.Background(), token, "  Invited@Acme.com "); err != nil {
		t.Errorf("case/whitespace variation should still match: %v", err)
	}
}

func TestResolveInvite_RejectsRevokedExpiredAndUnknown(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)
	ctx := context.Background()

	if _, err := svc.ResolveInvite(ctx, "inv_deadbeef", "a@acme.com"); err != ErrInviteNotFound {
		t.Errorf("unknown token: err = %v, want ErrInviteNotFound", err)
	}

	revoked, tokenR, _ := svc.CreateInvite(ctx, orgID, adminID, models.CreateInviteRequest{Email: "r@acme.com"})
	if err := svc.RevokeInvite(ctx, orgID, revoked.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if _, err := svc.ResolveInvite(ctx, tokenR, "r@acme.com"); err != ErrInviteNotFound {
		t.Errorf("revoked: err = %v, want ErrInviteNotFound", err)
	}

	expired, tokenE, _ := svc.CreateInvite(ctx, orgID, adminID, models.CreateInviteRequest{Email: "e@acme.com"})
	expired.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if _, err := svc.ResolveInvite(ctx, tokenE, "e@acme.com"); err != ErrInviteNotFound {
		t.Errorf("expired: err = %v, want ErrInviteNotFound", err)
	}
}

// An admin of one organization must not be able to revoke another's invite by
// guessing its id.
func TestRevokeInvite_RefusesCrossOrganization(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgA, adminA := seedOrgWithAdmin(repo)
	orgB := uuid.New().String()
	repo.orgs[orgB] = &models.Organization{ID: orgB, Slug: "other", IsActive: true}

	invite, _, _ := svc.CreateInvite(context.Background(), orgA, adminA,
		models.CreateInviteRequest{Email: "x@acme.com"})

	if err := svc.RevokeInvite(context.Background(), orgB, invite.ID); err != ErrInviteNotFound {
		t.Errorf("err = %v, want ErrInviteNotFound", err)
	}
	if repo.invites[invite.ID].RevokedAt != nil {
		t.Error("the invite was revoked from another organization")
	}
}

// ── members ──────────────────────────────────────────────────────────────────

// An organization with no admin has nobody who can invite, promote, or change
// its configuration — it is permanently stranded.
func TestUpdateMemberRole_RefusesToDemoteLastAdmin(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	// A second user does the demoting, so this is not caught by the self-guard.
	otherAdmin := uuid.New().String()
	repo.users[otherAdmin] = &models.User{ID: otherAdmin, Email: "b@acme.com"}
	repo.members[orgID+":"+otherAdmin] = &models.OrganizationMember{
		OrganizationID: orgID, UserID: otherAdmin, Role: models.RoleDeveloper, IsActive: true,
	}

	err := svc.UpdateMemberRole(context.Background(), orgID, otherAdmin, adminID, models.RoleDeveloper)
	if err != ErrLastAdmin {
		t.Fatalf("err = %v, want ErrLastAdmin", err)
	}
	if repo.members[orgID+":"+adminID].Role != models.RoleAdmin {
		t.Error("the last admin was demoted anyway")
	}
}

func TestRemoveMember_RefusesToRemoveLastAdmin(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	other := uuid.New().String()
	repo.users[other] = &models.User{ID: other, Email: "b@acme.com"}
	repo.members[orgID+":"+other] = &models.OrganizationMember{
		OrganizationID: orgID, UserID: other, Role: models.RoleDeveloper, IsActive: true,
	}

	if err := svc.RemoveMember(context.Background(), orgID, other, adminID); err != ErrLastAdmin {
		t.Fatalf("err = %v, want ErrLastAdmin", err)
	}
	if _, still := repo.members[orgID+":"+adminID]; !still {
		t.Error("the last admin was removed anyway")
	}
}

func TestUpdateMemberRole_AllowsDemotionWhenAnotherAdminRemains(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	second := uuid.New().String()
	repo.users[second] = &models.User{ID: second, Email: "b@acme.com"}
	repo.members[orgID+":"+second] = &models.OrganizationMember{
		OrganizationID: orgID, UserID: second, Role: models.RoleAdmin, IsActive: true,
	}

	if err := svc.UpdateMemberRole(context.Background(), orgID, second, adminID, models.RoleViewer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := repo.members[orgID+":"+adminID].Role; got != models.RoleViewer {
		t.Errorf("role = %q, want viewer", got)
	}
}

func TestMemberMutations_RefuseSelfChanges(t *testing.T) {
	repo := newMockMembershipRepo()
	svc := NewMembershipService(repo)
	orgID, adminID := seedOrgWithAdmin(repo)

	if err := svc.UpdateMemberRole(context.Background(), orgID, adminID, adminID, models.RoleViewer); err != ErrSelfDemotion {
		t.Errorf("self demote: err = %v, want ErrSelfDemotion", err)
	}
	if err := svc.RemoveMember(context.Background(), orgID, adminID, adminID); err != ErrSelfDemotion {
		t.Errorf("self remove: err = %v, want ErrSelfDemotion", err)
	}
}
