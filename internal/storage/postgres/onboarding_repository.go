package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/gorm"
)

// ── flows ────────────────────────────────────────────────────────────────────

func (pr *PostgresRepository) CreateOnboardingFlow(ctx context.Context, flow *models.OnboardingFlow) error {
	if err := pr.db.WithContext(ctx).Create(flow).Error; err != nil {
		return fmt.Errorf("create onboarding flow: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetOnboardingFlow(ctx context.Context, id string) (*models.OnboardingFlow, error) {
	var flow models.OnboardingFlow
	if err := pr.db.WithContext(ctx).First(&flow, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get onboarding flow: %w", err)
	}
	return &flow, nil
}

func (pr *PostgresRepository) GetOnboardingFlowBySlug(ctx context.Context, orgID, slug string) (*models.OnboardingFlow, error) {
	var flow models.OnboardingFlow
	err := pr.db.WithContext(ctx).
		First(&flow, "organization_id = ? AND slug = ? AND deleted_at IS NULL", orgID, slug).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get onboarding flow by slug: %w", err)
	}
	return &flow, nil
}

func (pr *PostgresRepository) GetDefaultOnboardingFlow(ctx context.Context, orgID string) (*models.OnboardingFlow, error) {
	var flow models.OnboardingFlow
	err := pr.db.WithContext(ctx).
		First(&flow, "organization_id = ? AND is_default AND deleted_at IS NULL", orgID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// No default is a normal state: the organization simply has not
			// nominated one, and invites without a flow produce no assignment.
			return nil, nil
		}
		return nil, fmt.Errorf("get default onboarding flow: %w", err)
	}
	return &flow, nil
}

func (pr *PostgresRepository) ListOnboardingFlows(ctx context.Context, orgID string) ([]models.OnboardingFlow, error) {
	var flows []models.OnboardingFlow
	if err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND deleted_at IS NULL", orgID).
		Order("is_default DESC, name ASC").
		Find(&flows).Error; err != nil {
		return nil, fmt.Errorf("list onboarding flows: %w", err)
	}
	return flows, nil
}

func (pr *PostgresRepository) UpdateOnboardingFlow(ctx context.Context, flow *models.OnboardingFlow) error {
	flow.UpdatedAt = time.Now().UTC()
	err := pr.db.WithContext(ctx).
		Model(&models.OnboardingFlow{}).
		Where("id = ? AND deleted_at IS NULL", flow.ID).
		Updates(map[string]interface{}{
			"name":        flow.Name,
			"slug":        flow.Slug,
			"description": flow.Description,
			"is_default":  flow.IsDefault,
			"updated_at":  flow.UpdatedAt,
		}).Error
	if err != nil {
		return fmt.Errorf("update onboarding flow: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) ClearDefaultOnboardingFlow(ctx context.Context, orgID, exceptFlowID string) error {
	query := pr.db.WithContext(ctx).
		Model(&models.OnboardingFlow{}).
		Where("organization_id = ? AND is_default AND deleted_at IS NULL", orgID)
	if exceptFlowID != "" {
		query = query.Where("id <> ?", exceptFlowID)
	}
	if err := query.Updates(map[string]interface{}{
		"is_default": false,
		"updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return fmt.Errorf("clear default onboarding flow: %w", err)
	}
	return nil
}

// DeleteOnboardingFlow soft-deletes the flow. Its steps and assignments stay,
// which keeps the history of who walked what — reading them goes through the
// flow, and a deleted flow is not listed.
func (pr *PostgresRepository) DeleteOnboardingFlow(ctx context.Context, id string) error {
	now := time.Now().UTC()
	err := pr.db.WithContext(ctx).
		Model(&models.OnboardingFlow{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]interface{}{
			"deleted_at": now,
			"updated_at": now,
			// A deleted flow must not stay the organization's default, or the
			// partial unique index would block nominating a replacement.
			"is_default": false,
		}).Error
	if err != nil {
		return fmt.Errorf("delete onboarding flow: %w", err)
	}
	return nil
}

// ── steps ────────────────────────────────────────────────────────────────────

func (pr *PostgresRepository) ListOnboardingSteps(ctx context.Context, flowID string) ([]models.OnboardingStep, error) {
	var steps []models.OnboardingStep
	if err := pr.db.WithContext(ctx).
		Where("flow_id = ?", flowID).
		Order("position ASC").
		Find(&steps).Error; err != nil {
		return nil, fmt.Errorf("list onboarding steps: %w", err)
	}
	return steps, nil
}

// stepCount is the projection of the grouped count query.
type stepCount struct {
	FlowID string
	Count  int
}

func (pr *PostgresRepository) CountOnboardingStepsByFlow(ctx context.Context, flowIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(flowIDs))
	if len(flowIDs) == 0 {
		return counts, nil
	}

	var rows []stepCount
	if err := pr.db.WithContext(ctx).
		Table("onboarding_steps").
		Select("flow_id, COUNT(*) AS count").
		Where("flow_id IN ?", flowIDs).
		Group("flow_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("count onboarding steps: %w", err)
	}
	for i := range rows {
		counts[rows[i].FlowID] = rows[i].Count
	}
	return counts, nil
}

func (pr *PostgresRepository) GetOnboardingStep(ctx context.Context, id string) (*models.OnboardingStep, error) {
	var step models.OnboardingStep
	if err := pr.db.WithContext(ctx).First(&step, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get onboarding step: %w", err)
	}
	return &step, nil
}

// ReplaceOnboardingSteps saves the whole list in one transaction.
//
// The important part is what it does NOT do: it never deletes and re-inserts a
// step that already exists. Progress rows reference `step_id` with
// ON DELETE CASCADE, so recreating steps on every save would erase everyone's
// progress each time an admin fixed a typo — which is exactly the failure that
// makes "live edits" unacceptable in most onboarding tools. Existing ids are
// updated in place; only steps genuinely dropped from the list are deleted.
func (pr *PostgresRepository) ReplaceOnboardingSteps(ctx context.Context, flowID string, steps []models.OnboardingStep) error {
	err := pr.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []models.OnboardingStep
		if err := tx.Where("flow_id = ?", flowID).Find(&existing).Error; err != nil {
			return fmt.Errorf("load existing steps: %w", err)
		}

		keep := make(map[string]bool, len(steps))
		for i := range steps {
			if steps[i].ID != "" {
				keep[steps[i].ID] = true
			}
		}

		var removed []string
		for i := range existing {
			if !keep[existing[i].ID] {
				removed = append(removed, existing[i].ID)
			}
		}
		if len(removed) > 0 {
			if err := tx.Where("id IN ?", removed).Delete(&models.OnboardingStep{}).Error; err != nil {
				return fmt.Errorf("delete removed steps: %w", err)
			}
		}

		now := time.Now().UTC()
		for i := range steps {
			step := &steps[i]
			// Position always comes from slice order, so reordering needs no
			// separate endpoint and no uniqueness dance.
			step.Position = i
			step.FlowID = flowID
			step.UpdatedAt = now

			if step.ID == "" {
				step.CreatedAt = now
				if err := tx.Create(step).Error; err != nil {
					return fmt.Errorf("create step %q: %w", step.Title, err)
				}
				continue
			}

			result := tx.Model(&models.OnboardingStep{}).
				Where("id = ? AND flow_id = ?", step.ID, flowID).
				Updates(map[string]interface{}{
					"position":          step.Position,
					"kind":              step.Kind,
					"title":             step.Title,
					"body":              step.Body,
					"config":            step.Config,
					"is_required":       step.IsRequired,
					"estimated_minutes": step.EstimatedMinutes,
					"updated_at":        step.UpdatedAt,
				})
			if result.Error != nil {
				return fmt.Errorf("update step %q: %w", step.Title, result.Error)
			}
			// An id that matches nothing in this flow is a caller mistake —
			// either a step from another flow or one already deleted. Creating
			// it silently would move a step across flows.
			if result.RowsAffected == 0 {
				return fmt.Errorf("step %s does not belong to flow %s", step.ID, flowID)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("replace onboarding steps: %w", err)
	}
	return nil
}

// ── assignments ──────────────────────────────────────────────────────────────

func (pr *PostgresRepository) CreateOnboardingAssignment(ctx context.Context, assignment *models.OnboardingAssignment) error {
	if err := pr.db.WithContext(ctx).Create(assignment).Error; err != nil {
		return fmt.Errorf("create onboarding assignment: %w", err)
	}
	return nil
}

func (pr *PostgresRepository) GetOnboardingAssignment(ctx context.Context, id string) (*models.OnboardingAssignment, error) {
	var assignment models.OnboardingAssignment
	if err := pr.db.WithContext(ctx).First(&assignment, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get onboarding assignment: %w", err)
	}
	return &assignment, nil
}

func (pr *PostgresRepository) ListOnboardingAssignmentsForUser(ctx context.Context, orgID, userID string) ([]models.OnboardingAssignment, error) {
	var assignments []models.OnboardingAssignment
	err := pr.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ? AND status <> ?", orgID, userID, models.OnboardingAssignmentAbandoned).
		Order("created_at DESC").
		Find(&assignments).Error
	if err != nil {
		return nil, fmt.Errorf("list onboarding assignments for user: %w", err)
	}
	return assignments, nil
}

func (pr *PostgresRepository) ListOnboardingAssignments(ctx context.Context, orgID string) ([]models.OnboardingAssignment, error) {
	var assignments []models.OnboardingAssignment
	err := pr.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Order("created_at DESC").
		Find(&assignments).Error
	if err != nil {
		return nil, fmt.Errorf("list onboarding assignments: %w", err)
	}
	return assignments, nil
}

func (pr *PostgresRepository) UpdateOnboardingAssignment(ctx context.Context, assignment *models.OnboardingAssignment) error {
	assignment.UpdatedAt = time.Now().UTC()
	err := pr.db.WithContext(ctx).
		Model(&models.OnboardingAssignment{}).
		Where("id = ?", assignment.ID).
		Updates(map[string]interface{}{
			"status":       assignment.Status,
			"started_at":   assignment.StartedAt,
			"completed_at": assignment.CompletedAt,
			"feedback":     assignment.Feedback,
			"feedback_at":  assignment.FeedbackAt,
			"updated_at":   assignment.UpdatedAt,
		}).Error
	if err != nil {
		return fmt.Errorf("update onboarding assignment: %w", err)
	}
	return nil
}

// ── progress ─────────────────────────────────────────────────────────────────

func (pr *PostgresRepository) ListOnboardingStepProgress(ctx context.Context, assignmentID string) ([]models.OnboardingStepProgress, error) {
	var progress []models.OnboardingStepProgress
	if err := pr.db.WithContext(ctx).
		Where("assignment_id = ?", assignmentID).
		Find(&progress).Error; err != nil {
		return nil, fmt.Errorf("list onboarding step progress: %w", err)
	}
	return progress, nil
}

// UpsertOnboardingStepProgress records an outcome, overwriting any previous one
// for the same step — marking a skipped step done later is an ordinary thing to
// do, not a conflict.
func (pr *PostgresRepository) UpsertOnboardingStepProgress(ctx context.Context, progress *models.OnboardingStepProgress) error {
	now := time.Now().UTC()
	if progress.CompletedAt.IsZero() {
		progress.CompletedAt = now
	}
	progress.UpdatedAt = now

	err := pr.db.WithContext(ctx).Exec(`
        INSERT INTO onboarding_step_progress
            (assignment_id, step_id, status, note, completed_at, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (assignment_id, step_id) DO UPDATE SET
            status       = EXCLUDED.status,
            note         = EXCLUDED.note,
            completed_at = EXCLUDED.completed_at,
            updated_at   = EXCLUDED.updated_at
    `, progress.AssignmentID, progress.StepID, progress.Status, progress.Note,
		progress.CompletedAt, now, progress.UpdatedAt).Error
	if err != nil {
		return fmt.Errorf("upsert onboarding step progress: %w", err)
	}
	return nil
}
