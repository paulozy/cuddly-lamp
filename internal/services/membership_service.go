package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

const (
	invitePrefix    = "inv_"
	inviteRandBytes = 32
	// defaultInviteTTL keeps a leaked link from being useful forever. A week is
	// long enough to survive a weekend and short enough to matter.
	defaultInviteTTL = 7 * 24 * time.Hour
	maxInviteTTL     = 30 * 24 * time.Hour
)

var (
	ErrInviteInvalidEmail  = errors.New("invite requires a valid email address")
	ErrInviteInvalidRole   = errors.New("invite role must be viewer, developer, maintainer or admin")
	ErrInviteNotFound      = errors.New("invite not found")
	ErrInviteAlreadyMember = errors.New("that address already belongs to this organization")
	ErrMemberNotFound      = errors.New("member not found")
	// ErrLastAdmin guards against locking everyone out of an organization: an org
	// with no admin has no one who can invite, promote, or change its config.
	ErrLastAdmin    = errors.New("cannot remove or demote the last admin of the organization")
	ErrSelfDemotion = errors.New("you cannot change your own role")
)

// MembershipService owns who belongs to an organization and how they got there.
type MembershipService struct {
	repo storage.Repository
}

func NewMembershipService(repo storage.Repository) *MembershipService {
	return &MembershipService{repo: repo}
}

// ── invites ──────────────────────────────────────────────────────────────────

// CreateInvite mints a single-use invite bound to an e-mail address and returns
// the plaintext token exactly once. Only the hash is stored, mirroring how
// coverage upload tokens work.
func (s *MembershipService) CreateInvite(
	ctx context.Context,
	orgID, createdByUserID string,
	req models.CreateInviteRequest,
) (*models.OrganizationInvite, string, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, "", ErrInviteInvalidEmail
	}

	role := req.Role
	if role == "" {
		role = models.RoleDeveloper
	}
	if !isAssignableRole(role) {
		return nil, "", ErrInviteInvalidRole
	}

	// Inviting someone who is already in is a no-op at best and confusing at
	// worst — the invite would be redeemable but redundant.
	members, err := s.repo.ListOrganizationMembers(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	for i := range members {
		if strings.EqualFold(members[i].User.Email, email) {
			return nil, "", ErrInviteAlreadyMember
		}
	}

	ttl := defaultInviteTTL
	if req.TTLHours > 0 {
		requested := time.Duration(req.TTLHours) * time.Hour
		if requested > maxInviteTTL {
			requested = maxInviteTTL
		}
		ttl = requested
	}

	plain, err := generateInviteToken()
	if err != nil {
		return nil, "", fmt.Errorf("generate invite token: %w", err)
	}

	invite := &models.OrganizationInvite{
		OrganizationID: orgID,
		Email:          email,
		Role:           role,
		TokenHash:      HashInviteToken(plain),
		ExpiresAt:      time.Now().UTC().Add(ttl),
	}

	// An invite may name the onboarding the person should walk. It has to be
	// this organization's flow: pointing at somebody else's would hand a
	// newcomer a tour of a company they did not join.
	if flowID := strings.TrimSpace(req.OnboardingFlowID); flowID != "" {
		flow, err := s.repo.GetOnboardingFlow(ctx, flowID)
		if err != nil {
			return nil, "", err
		}
		if flow == nil || flow.OrganizationID != orgID {
			return nil, "", ErrOnboardingFlowNotFound
		}
		invite.OnboardingFlowID = &flowID
	}
	if createdByUserID != "" {
		invite.CreatedByUserID = &createdByUserID
	}

	if err := s.repo.CreateOrganizationInvite(ctx, invite); err != nil {
		return nil, "", err
	}
	return invite, plain, nil
}

func (s *MembershipService) ListInvites(ctx context.Context, orgID string) ([]models.OrganizationInvite, error) {
	return s.repo.ListOrganizationInvites(ctx, orgID)
}

// RevokeInvite refuses to act across organizations: an admin of org A must not be
// able to revoke org B's invite by guessing its id.
func (s *MembershipService) RevokeInvite(ctx context.Context, orgID, inviteID string) error {
	invite, err := s.repo.GetOrganizationInvite(ctx, inviteID)
	if err != nil {
		return err
	}
	if invite == nil || invite.OrganizationID != orgID {
		return ErrInviteNotFound
	}
	if err := s.repo.RevokeOrganizationInvite(ctx, inviteID); err != nil {
		return ErrInviteNotFound
	}
	return nil
}

// ResolveInvite validates a plaintext token for a given e-mail and returns the
// invite when it may be redeemed. Every failure mode collapses to the same error
// so the endpoint cannot be used to probe which organizations or addresses exist.
func (s *MembershipService) ResolveInvite(ctx context.Context, token, email string) (*models.OrganizationInvite, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInviteNotFound
	}
	invite, err := s.repo.GetOrganizationInviteByHash(ctx, HashInviteToken(token))
	if err != nil {
		return nil, err
	}
	if invite == nil {
		return nil, ErrInviteNotFound
	}
	if !invite.IsRedeemable(time.Now().UTC()) {
		return nil, ErrInviteNotFound
	}
	// The binding to an address is what stops a forwarded link from being a
	// general-purpose key to the organization.
	if !invite.MatchesEmail(email) {
		return nil, ErrInviteNotFound
	}
	return invite, nil
}

// ── members ──────────────────────────────────────────────────────────────────

func (s *MembershipService) ListMembers(ctx context.Context, orgID string) ([]models.MemberResponse, error) {
	members, err := s.repo.ListOrganizationMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]models.MemberResponse, 0, len(members))
	for i := range members {
		m := &members[i]
		out = append(out, models.MemberResponse{
			UserID:   m.UserID,
			Email:    m.User.Email,
			FullName: m.User.FullName,
			Role:     m.Role,
			IsActive: m.IsActive,
			JoinedAt: m.CreatedAt,
		})
	}
	return out, nil
}

func (s *MembershipService) UpdateMemberRole(ctx context.Context, orgID, actingUserID, targetUserID string, role models.UserRole) error {
	if !isAssignableRole(role) {
		return ErrInviteInvalidRole
	}
	// Self-demotion is how an organization loses its last admin by accident.
	if actingUserID == targetUserID {
		return ErrSelfDemotion
	}

	member, err := s.repo.GetOrganizationMember(ctx, orgID, targetUserID)
	if err != nil {
		return err
	}
	if member == nil {
		return ErrMemberNotFound
	}
	if member.Role == role {
		return nil
	}
	if member.Role == models.RoleAdmin {
		if err := s.ensureNotLastAdmin(ctx, orgID); err != nil {
			return err
		}
	}
	return s.repo.UpdateOrganizationMemberRole(ctx, orgID, targetUserID, role)
}

func (s *MembershipService) RemoveMember(ctx context.Context, orgID, actingUserID, targetUserID string) error {
	if actingUserID == targetUserID {
		return ErrSelfDemotion
	}
	member, err := s.repo.GetOrganizationMember(ctx, orgID, targetUserID)
	if err != nil {
		return err
	}
	if member == nil {
		return ErrMemberNotFound
	}
	if member.Role == models.RoleAdmin {
		if err := s.ensureNotLastAdmin(ctx, orgID); err != nil {
			return err
		}
	}
	return s.repo.DeleteOrganizationMember(ctx, orgID, targetUserID)
}

func (s *MembershipService) ensureNotLastAdmin(ctx context.Context, orgID string) error {
	admins, err := s.repo.CountOrganizationAdmins(ctx, orgID)
	if err != nil {
		return err
	}
	if admins <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func isAssignableRole(role models.UserRole) bool {
	switch role {
	case models.RoleViewer, models.RoleDeveloper, models.RoleMaintainer, models.RoleAdmin:
		return true
	default:
		return false
	}
}

func generateInviteToken() (string, error) {
	buf := make([]byte, inviteRandBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return invitePrefix + hex.EncodeToString(buf), nil
}

// HashInviteToken is exported so the auth service can look up an invite during
// registration without duplicating the hashing scheme.
func HashInviteToken(plain string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(plain)))
	return hex.EncodeToString(sum[:])
}
