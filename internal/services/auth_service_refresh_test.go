package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// mockRepo implements storage.Repository with only token-related methods functional.
type mockRepo struct {
	storage.Repository // embed to satisfy non-implemented methods at compile time

	tokens  map[string]*models.Token // keyed by token_hash
	users   map[string]*models.User  // keyed by user_id
	orgs    map[string]*models.Organization
	members map[string]*models.OrganizationMember
	configs map[string]*models.OrganizationConfig

	revokedJTIs     []string
	revokedFamilies []uuid.UUID
	createError     error
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		tokens:  make(map[string]*models.Token),
		users:   make(map[string]*models.User),
		orgs:    make(map[string]*models.Organization),
		members: make(map[string]*models.OrganizationMember),
		configs: make(map[string]*models.OrganizationConfig),
	}
}

func (m *mockRepo) CreateToken(_ context.Context, token *models.Token) error {
	if m.createError != nil {
		return m.createError
	}
	m.tokens[token.TokenHash] = token
	return nil
}

func (m *mockRepo) GetTokenByJTI(_ context.Context, jti string) (*models.Token, error) {
	for _, t := range m.tokens {
		if t.JTI == jti {
			return t, nil
		}
	}
	return nil, nil
}

func (m *mockRepo) GetTokenByHash(_ context.Context, tokenHash string) (*models.Token, error) {
	t, ok := m.tokens[tokenHash]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func (m *mockRepo) RevokeToken(_ context.Context, jti string, _ string) error {
	m.revokedJTIs = append(m.revokedJTIs, jti)
	for _, t := range m.tokens {
		if t.JTI == jti {
			t.IsRevoked = true
		}
	}
	return nil
}

func (m *mockRepo) RevokeTokenFamily(_ context.Context, familyID uuid.UUID, _ string) error {
	m.revokedFamilies = append(m.revokedFamilies, familyID)
	for _, t := range m.tokens {
		if t.FamilyID != nil && *t.FamilyID == familyID {
			t.IsRevoked = true
		}
	}
	return nil
}

func (m *mockRepo) UpdateTokenLastUsed(_ context.Context, _ string) error { return nil }

func (m *mockRepo) GetUser(_ context.Context, id string) (*models.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

func (m *mockRepo) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, nil
}

func (m *mockRepo) CreateUser(_ context.Context, user *models.User) error {
	m.users[user.ID] = user
	return nil
}

func (m *mockRepo) GetOrganization(_ context.Context, id string) (*models.Organization, error) {
	org, ok := m.orgs[id]
	if !ok {
		return nil, nil
	}
	return org, nil
}

func (m *mockRepo) GetOrganizationBySlug(_ context.Context, slug string) (*models.Organization, error) {
	for _, org := range m.orgs {
		if org.Slug == slug {
			return org, nil
		}
	}
	return nil, nil
}

func (m *mockRepo) CreateOrganization(_ context.Context, org *models.Organization) error {
	m.orgs[org.ID] = org
	return nil
}

func (m *mockRepo) GetOrganizationMember(_ context.Context, orgID, userID string) (*models.OrganizationMember, error) {
	member, ok := m.members[orgID+":"+userID]
	if !ok {
		return nil, nil
	}
	return member, nil
}

func (m *mockRepo) ListOrganizationMembersForUser(_ context.Context, userID string) ([]models.OrganizationMember, error) {
	var members []models.OrganizationMember
	for _, member := range m.members {
		if member.UserID != userID || !member.IsActive {
			continue
		}
		copy := *member
		if org, ok := m.orgs[member.OrganizationID]; ok {
			copy.Organization = *org
		}
		members = append(members, copy)
	}
	return members, nil
}

func (m *mockRepo) CountOrganizationMembers(_ context.Context, orgID string) (int64, error) {
	var count int64
	for _, member := range m.members {
		if member.OrganizationID == orgID {
			count++
		}
	}
	return count, nil
}

func (m *mockRepo) CreateOrganizationMember(_ context.Context, member *models.OrganizationMember) error {
	m.members[member.OrganizationID+":"+member.UserID] = member
	return nil
}

func (m *mockRepo) GetOrganizationConfig(_ context.Context, orgID string) (*models.OrganizationConfig, error) {
	cfg, ok := m.configs[orgID]
	if !ok {
		return nil, nil
	}
	return cfg, nil
}

func (m *mockRepo) UpsertOrganizationConfig(_ context.Context, cfg *models.OrganizationConfig) error {
	m.configs[cfg.OrganizationID] = cfg
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func newTestService(repo *mockRepo) *AuthService {
	return NewAuthService(
		repo,
		"test-secret-key-for-testing-only",
		"test-issuer",
		"test-audience",
		15*time.Minute,
		7*24*time.Hour,
	)
}

func newTestUser() *models.User {
	id := uuid.New().String()
	return &models.User{
		ID:       id,
		Email:    "test@example.com",
		FullName: "Test User",
		Role:     models.RoleDeveloper,
		IsActive: true,
	}
}

func newTestOrg() *models.Organization {
	return &models.Organization{
		ID:       uuid.New().String(),
		Name:     "Test Org",
		Slug:     "test-org",
		IsActive: true,
	}
}

// seedRefreshToken mints a real refresh token via the service and returns its raw value.
func seedRefreshToken(t *testing.T, svc *AuthService, repo *mockRepo, user *models.User) string {
	t.Helper()
	org := newTestOrg()
	repo.users[user.ID] = user
	repo.orgs[org.ID] = org
	repo.members[org.ID+":"+user.ID] = &models.OrganizationMember{
		OrganizationID: org.ID,
		UserID:         user.ID,
		Role:           models.RoleDeveloper,
		IsActive:       true,
	}
	resp, err := svc.generateTokenPair(context.Background(), user, org, models.RoleDeveloper)
	if err != nil {
		t.Fatalf("seedRefreshToken: %v", err)
	}
	return resp.RefreshToken
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestRefreshTokens_ValidToken_ReturnsNewPair(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	user := newTestUser()

	rawRT := seedRefreshToken(t, svc, repo, user)

	resp, err := svc.RefreshTokens(context.Background(), rawRT)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if resp.RefreshToken == rawRT {
		t.Error("new refresh token must differ from old one (rotation)")
	}
}

func TestRefreshTokens_ExpiredToken_ReturnsErrTokenExpired(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	user := newTestUser()
	repo.users[user.ID] = user

	rawRT := seedRefreshToken(t, svc, repo, user)

	// Backdate the stored refresh token's expiry
	rtHash := hashToken(rawRT)
	repo.tokens[rtHash].ExpiresAt = time.Now().UTC().Add(-1 * time.Hour)

	_, err := svc.RefreshTokens(context.Background(), rawRT)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestRefreshTokens_RevokedToken_RevokesFamily(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	user := newTestUser()
	repo.users[user.ID] = user

	rawRT := seedRefreshToken(t, svc, repo, user)

	// Mark as already revoked (simulates a reuse attempt after rotation)
	rtHash := hashToken(rawRT)
	stored := repo.tokens[rtHash]
	stored.IsRevoked = true

	_, err := svc.RefreshTokens(context.Background(), rawRT)
	if !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("expected ErrTokenRevoked, got: %v", err)
	}
	if len(repo.revokedFamilies) == 0 {
		t.Error("expected family to be revoked on reuse detection")
	}
}

func TestRefreshTokens_UnknownToken_ReturnsErrInvalidToken(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, err := svc.RefreshTokens(context.Background(), "completely-fake-token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got: %v", err)
	}
}

func TestRefreshTokens_AccessTokenUsedAsRefresh_ReturnsErrInvalidToken(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	user := newTestUser()
	repo.users[user.ID] = user

	org := newTestOrg()
	repo.orgs[org.ID] = org
	repo.members[org.ID+":"+user.ID] = &models.OrganizationMember{OrganizationID: org.ID, UserID: user.ID, Role: models.RoleDeveloper, IsActive: true}
	resp, err := svc.generateTokenPair(context.Background(), user, org, models.RoleDeveloper)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// The access token is a JWT; its hash won't match a "refresh" type record.
	// But let's explicitly plant it as a wrong-type token for a realistic test.
	accessHash := hashToken(resp.AccessToken)
	// The access token record is already stored with Type="access".
	// Attempting to refresh with it should return ErrInvalidToken (wrong type).
	for _, tok := range repo.tokens {
		if tok.TokenHash == accessHash {
			tok.Type = "access" // ensure it is access
		}
	}

	_, err = svc.RefreshTokens(context.Background(), resp.AccessToken)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken when access token used as refresh, got: %v", err)
	}
}

func TestValidateToken_RefreshTokenUsedAsBearer_ReturnsError(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	user := newTestUser()
	repo.users[user.ID] = user

	org := newTestOrg()
	repo.orgs[org.ID] = org
	repo.members[org.ID+":"+user.ID] = &models.OrganizationMember{OrganizationID: org.ID, UserID: user.ID, Role: models.RoleDeveloper, IsActive: true}
	resp, err := svc.generateTokenPair(context.Background(), user, org, models.RoleDeveloper)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Plant the refresh token's hash as an "access" type with a JWT-looking value
	// by replacing one of the access token records with type="refresh".
	// More directly: change the type of the stored access token so ValidateToken hits the type check.
	for _, tok := range repo.tokens {
		if tok.Type == "access" {
			tok.Type = "refresh"
		}
	}

	_, err = svc.ValidateToken(context.Background(), resp.AccessToken)
	if err == nil {
		t.Error("expected error when refresh token type found in ValidateToken, got nil")
	}
}

func TestRefreshTokens_OldTokenConsumedAfterRotation(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	user := newTestUser()

	rawRT := seedRefreshToken(t, svc, repo, user)

	// First rotation succeeds
	resp, err := svc.RefreshTokens(context.Background(), rawRT)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// Original token must now be revoked in the store
	rtHash := hashToken(rawRT)
	if !repo.tokens[rtHash].IsRevoked {
		t.Error("old refresh token should be revoked after rotation")
	}

	// Second use of the new token must succeed
	_, err = svc.RefreshTokens(context.Background(), resp.RefreshToken)
	if err != nil {
		t.Errorf("second rotation with new token should succeed, got: %v", err)
	}
}

func TestGenerateTokenPair_ResponseContainsRefreshFields(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	user := newTestUser()
	repo.users[user.ID] = user

	org := newTestOrg()
	repo.orgs[org.ID] = org
	repo.members[org.ID+":"+user.ID] = &models.OrganizationMember{OrganizationID: org.ID, UserID: user.ID, Role: models.RoleDeveloper, IsActive: true}
	resp, err := svc.generateTokenPair(context.Background(), user, org, models.RoleDeveloper)
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}

	if resp.RefreshToken == "" {
		t.Error("refresh_token must not be empty")
	}
	if resp.RefreshExpiresIn <= 0 {
		t.Error("refresh_expires_in must be positive")
	}
	if resp.AccessToken == "" {
		t.Error("access_token must not be empty")
	}
	if resp.ExpiresIn <= 0 {
		t.Error("expires_in must be positive")
	}
}

func TestRegisterWithEmail_CreatesOrganizationFromBody(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	resp, err := svc.RegisterWithEmail(context.Background(), "new@example.com", "New User", "Password123", "Acme Inc", "")
	if err != nil {
		t.Fatalf("RegisterWithEmail failed: %v", err)
	}

	if resp.Organization.Name != "Acme Inc" {
		t.Fatalf("organization name = %q, want Acme Inc", resp.Organization.Name)
	}
	if resp.Organization.Slug != "acme-inc" {
		t.Fatalf("organization slug = %q, want acme-inc", resp.Organization.Slug)
	}
	if resp.Organization.Role != models.RoleAdmin {
		t.Fatalf("organization role = %q, want admin", resp.Organization.Role)
	}
	if _, ok := repo.configs[resp.Organization.ID]; !ok {
		t.Fatal("expected default organization config to be created")
	}
}

func TestLoginWithEmail_SingleOrganizationDoesNotRequireSlug(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, err := svc.RegisterWithEmail(context.Background(), "single@example.com", "Single User", "Password123", "Single Org", "")
	if err != nil {
		t.Fatalf("RegisterWithEmail failed: %v", err)
	}

	resp, selection, err := svc.LoginWithEmail(context.Background(), "single@example.com", "Password123", "")
	if err != nil {
		t.Fatalf("LoginWithEmail failed: %v", err)
	}
	if selection != nil {
		t.Fatalf("LoginWithEmail selection = %+v, want nil", selection)
	}
	if resp.Organization.Slug != "single-org" {
		t.Fatalf("organization slug = %q, want single-org", resp.Organization.Slug)
	}
}

func TestLoginWithEmail_MultipleOrganizationsReturnsSelectionTicket(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	if _, err := svc.RegisterWithEmail(context.Background(), "multi@example.com", "Multi User", "Password123", "First Org", "first"); err != nil {
		t.Fatalf("register first org: %v", err)
	}
	if _, err := svc.RegisterWithEmail(context.Background(), "multi@example.com", "Multi User", "Password123", "Second Org", "second"); err != nil {
		t.Fatalf("register second org: %v", err)
	}

	resp, selection, err := svc.LoginWithEmail(context.Background(), "multi@example.com", "Password123", "")
	if err != nil {
		t.Fatalf("LoginWithEmail failed: %v", err)
	}
	if resp != nil {
		t.Fatalf("LoginWithEmail token = %+v, want nil until organization is selected", resp)
	}
	if selection == nil {
		t.Fatal("LoginWithEmail selection = nil, want organization selection response")
	}
	if !selection.RequiresOrganizationSelection {
		t.Fatal("selection response should require organization selection")
	}
	if selection.LoginTicket == "" {
		t.Fatal("selection response should include login ticket")
	}
	if len(selection.Organizations) != 2 {
		t.Fatalf("selection organizations = %+v, want 2", selection.Organizations)
	}

	selected, err := svc.SelectOrganization(context.Background(), selection.LoginTicket, selection.Organizations[1].ID)
	if err != nil {
		t.Fatalf("SelectOrganization failed: %v", err)
	}
	if selected.Organization.ID != selection.Organizations[1].ID {
		t.Fatalf("selected organization = %q, want %q", selected.Organization.ID, selection.Organizations[1].ID)
	}
}

func TestSelectOrganizationRejectsUnavailableOrganization(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	if _, err := svc.RegisterWithEmail(context.Background(), "multi@example.com", "Multi User", "Password123", "First Org", "first"); err != nil {
		t.Fatalf("register first org: %v", err)
	}
	if _, err := svc.RegisterWithEmail(context.Background(), "multi@example.com", "Multi User", "Password123", "Second Org", "second"); err != nil {
		t.Fatalf("register second org: %v", err)
	}
	otherOrg := newTestOrg()
	otherOrg.Slug = "other"
	repo.orgs[otherOrg.ID] = otherOrg

	_, selection, err := svc.LoginWithEmail(context.Background(), "multi@example.com", "Password123", "")
	if err != nil {
		t.Fatalf("LoginWithEmail failed: %v", err)
	}
	if selection == nil {
		t.Fatal("LoginWithEmail selection = nil, want selection response")
	}

	_, err = svc.SelectOrganization(context.Background(), selection.LoginTicket, otherOrg.ID)
	if err == nil {
		t.Fatal("SelectOrganization should reject an organization outside the login ticket")
	}
}
