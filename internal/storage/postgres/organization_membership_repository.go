package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/gorm"
)

// ============ Organization Members ============

func (pr *PostgresRepository) ListOrganizationMembers(ctx context.Context, orgID string) ([]models.OrganizationMember, error) {
	var members []models.OrganizationMember
	if err := pr.db.WithContext(ctx).
		Preload("User").
		Where("organization_id = ?", orgID).
		Order("created_at ASC").
		Find(&members).Error; err != nil {
		return nil, fmt.Errorf("list organization members: %w", err)
	}
	return members, nil
}

// CountOrganizationAdmins backs the "never strand an organization" rule: the last
// admin cannot be demoted or removed.
func (pr *PostgresRepository) CountOrganizationAdmins(ctx context.Context, orgID string) (int64, error) {
	var count int64
	if err := pr.db.WithContext(ctx).
		Model(&models.OrganizationMember{}).
		Where("organization_id = ? AND role = ? AND is_active = ?", orgID, models.RoleAdmin, true).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count organization admins: %w", err)
	}
	return count, nil
}

func (pr *PostgresRepository) UpdateOrganizationMemberRole(ctx context.Context, orgID, userID string, role models.UserRole) error {
	res := pr.db.WithContext(ctx).
		Model(&models.OrganizationMember{}).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Updates(map[string]any{"role": role, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return fmt.Errorf("update organization member role: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (pr *PostgresRepository) DeleteOrganizationMember(ctx context.Context, orgID, userID string) error {
	res := pr.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Delete(&models.OrganizationMember{})
	if res.Error != nil {
		return fmt.Errorf("delete organization member: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ============ Organization Invites ============

func (pr *PostgresRepository) CreateOrganizationInvite(ctx context.Context, invite *models.OrganizationInvite) error {
	if err := pr.db.WithContext(ctx).Create(invite).Error; err != nil {
		return fmt.Errorf("create organization invite: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetOrganizationInviteByHash(ctx context.Context, hash string) (*models.OrganizationInvite, error) {
	var invite models.OrganizationInvite
	if err := pr.db.WithContext(ctx).First(&invite, "token_hash = ?", hash).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get organization invite by hash: %w", err)
	}
	return &invite, nil
}

func (pr *PostgresRepository) GetOrganizationInvite(ctx context.Context, id string) (*models.OrganizationInvite, error) {
	var invite models.OrganizationInvite
	if err := pr.db.WithContext(ctx).First(&invite, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get organization invite: %w", err)
	}
	return &invite, nil
}

func (pr *PostgresRepository) ListOrganizationInvites(ctx context.Context, orgID string) ([]models.OrganizationInvite, error) {
	var invites []models.OrganizationInvite
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Order("created_at DESC").
		Find(&invites).Error; err != nil {
		return nil, fmt.Errorf("list organization invites: %w", err)
	}
	return invites, nil
}

func (pr *PostgresRepository) RevokeOrganizationInvite(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res := pr.db.WithContext(ctx).
		Model(&models.OrganizationInvite{}).
		Where("id = ? AND accepted_at IS NULL AND revoked_at IS NULL", id).
		Update("revoked_at", now)
	if res.Error != nil {
		return fmt.Errorf("revoke organization invite: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		// Either the id is unknown or the invite was already spent/revoked. Both
		// are "nothing left to revoke" from the caller's point of view.
		return gorm.ErrRecordNotFound
	}
	return nil
}

// AcceptOrganizationInvite spends the invite and creates the membership atomically.
//
// The UPDATE is guarded on `accepted_at IS NULL`, so two concurrent redemptions of
// the same link race on the row and exactly one wins — the loser sees zero rows
// affected and the transaction rolls back. This is what makes an invite single-use
// under concurrency, not just in the happy path.
func (pr *PostgresRepository) AcceptOrganizationInvite(ctx context.Context, inviteID string, member *models.OrganizationMember) error {
	return pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		res := tx.Model(&models.OrganizationInvite{}).
			Where("id = ? AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", inviteID, now).
			Updates(map[string]any{
				"accepted_at":         now,
				"accepted_by_user_id": member.UserID,
			})
		if res.Error != nil {
			return fmt.Errorf("mark invite accepted: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrInviteNotRedeemable
		}
		if err := tx.Create(member).Error; err != nil {
			return fmt.Errorf("create organization member: %w", err)
		}
		return nil
	})
}

// ErrInviteNotRedeemable is returned when the guarded UPDATE matches no row —
// the invite was already accepted, revoked, or expired between validation and
// redemption.
var ErrInviteNotRedeemable = errors.New("organization invite is no longer redeemable")
