package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
)

// OnboardingFlow is a configurable path through the organization: what a new
// member should read, meet and do, in order.
//
// An organization has as many flows as it has kinds of newcomer (backend,
// frontend, data, management), which is why this is content rather than code.
type OnboardingFlow struct {
	ID             string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrganizationID string `gorm:"type:uuid;not null;index" json:"organization_id"`

	Name        string `gorm:"type:varchar(255);not null" json:"name"`
	Slug        string `gorm:"type:varchar(120);not null" json:"slug"`
	Description string `gorm:"type:text" json:"description,omitempty"`

	// IsDefault marks the flow an invite falls back to when it names none. At
	// most one per organization, enforced by a partial unique index.
	IsDefault bool `gorm:"not null;default:false" json:"is_default"`

	CreatedByUserID *string `gorm:"type:uuid" json:"created_by_user_id,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`

	// Steps is populated by the queries that read a flow for editing or for
	// walking it; it is not a GORM association, so a flow read on its own
	// carries none.
	Steps []OnboardingStep `gorm:"-" json:"steps,omitempty"`
}

func (OnboardingFlow) TableName() string {
	return "onboarding_flows"
}

// OnboardingStepKind decides what a step renders. Kinds fall into two groups:
// editorial ones carry their own prose in Body, and referential ones point at
// an entity the platform already knows about.
type OnboardingStepKind string

const (
	// Editorial.
	OnboardingStepKindMarkdown  OnboardingStepKind = "markdown"
	OnboardingStepKindChecklist OnboardingStepKind = "checklist"
	OnboardingStepKindLink      OnboardingStepKind = "link"
	OnboardingStepKindContacts  OnboardingStepKind = "contacts"
	OnboardingStepKindTask      OnboardingStepKind = "task"

	// Referential — rendered from live data, so they cannot go stale.
	OnboardingStepKindRepository   OnboardingStepKind = "repository"
	OnboardingStepKindTeam         OnboardingStepKind = "team"
	OnboardingStepKindDoc          OnboardingStepKind = "doc"
	OnboardingStepKindArchitecture OnboardingStepKind = "architecture"
	OnboardingStepKindGlossary     OnboardingStepKind = "glossary"

	// Checked by the platform rather than claimed by the person.
	OnboardingStepKindVerified OnboardingStepKind = "verified"
)

// IsKnown reports whether the kind is one this build can render. Unknown kinds
// are rejected on write rather than stored and skipped later, so a typo in the
// builder fails where it is made.
func (k OnboardingStepKind) IsKnown() bool {
	switch k {
	case OnboardingStepKindMarkdown, OnboardingStepKindChecklist, OnboardingStepKindLink,
		OnboardingStepKindContacts, OnboardingStepKindTask, OnboardingStepKindRepository,
		OnboardingStepKindTeam, OnboardingStepKindDoc, OnboardingStepKindArchitecture,
		OnboardingStepKindGlossary, OnboardingStepKindVerified:
		return true
	default:
		return false
	}
}

// OnboardingCompletionMode is how a step gets marked done. Keeping this on the
// kind — rather than letting each screen decide — is what stops the UI from
// claiming the platform verified something it merely watched someone click.
type OnboardingCompletionMode string

const (
	// OnboardingCompletionAuto closes when the person moves on: a page they read.
	OnboardingCompletionAuto OnboardingCompletionMode = "auto"
	// OnboardingCompletionAcknowledge needs an explicit click, for required
	// reading someone should not be able to scroll past by accident.
	OnboardingCompletionAcknowledge OnboardingCompletionMode = "acknowledge"
	// OnboardingCompletionSelfReported is the honest label for work the platform
	// cannot see: an external link, a checklist, the first task. The UI says
	// "marked by you".
	OnboardingCompletionSelfReported OnboardingCompletionMode = "self_reported"
	// OnboardingCompletionVerified is checked against real data, and the step
	// reports how it checked.
	OnboardingCompletionVerified OnboardingCompletionMode = "verified"
)

// CompletionMode maps a kind (and whether the step is required) onto how it is
// completed.
func (k OnboardingStepKind) CompletionMode(required bool) OnboardingCompletionMode {
	switch k {
	case OnboardingStepKindVerified:
		return OnboardingCompletionVerified
	case OnboardingStepKindChecklist, OnboardingStepKindLink, OnboardingStepKindTask:
		return OnboardingCompletionSelfReported
	default:
		if required {
			return OnboardingCompletionAcknowledge
		}
		return OnboardingCompletionAuto
	}
}

// OnboardingVerifiedCheck is what a verified step actually checks.
//
// The list is deliberately short. "Do you have access to repository X" is
// absent because answering it would mean calling the provider with the
// person's own OAuth token — invasive, and broken whenever that token expires.
// A step that sometimes fails someone who does have access is worse than an
// honest checkbox.
type OnboardingVerifiedCheck string

const (
	// OnboardingCheckFirstChangeRequest looks for a pull/merge request authored
	// by the person on the configured repository.
	OnboardingCheckFirstChangeRequest OnboardingVerifiedCheck = "first_change_request"
	// OnboardingCheckTeamMembership confirms the person belongs to a team — the
	// configured one, or any of them.
	OnboardingCheckTeamMembership OnboardingVerifiedCheck = "team_membership"
)

// OnboardingStepContact is one entry of a `contacts` step: a real person, plus
// the editorial bit about when to go to them.
//
// The mapping is stored on the step because it is editorial ("talk to Ana about
// access" is not a fact the system holds), but the person is a reference, so
// their name and role stay live.
type OnboardingStepContact struct {
	UserID      string `json:"user_id"`
	Area        string `json:"area"`
	WhenToReach string `json:"when_to_reach,omitempty"`
}

// OnboardingStepChecklistItem is one line of a `checklist` step.
type OnboardingStepChecklistItem struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// OnboardingStepConfig is the union of every kind's options. Only the fields
// belonging to the step's own kind are ever set, which is why this lives in
// JSONB rather than as columns that would be mostly NULL.
type OnboardingStepConfig struct {
	// repository, verified
	RepositoryID string `json:"repository_id,omitempty"`
	// team, verified
	TeamID string `json:"team_id,omitempty"`
	// doc
	DocGenerationID string `json:"doc_generation_id,omitempty"`
	DocType         string `json:"doc_type,omitempty"`
	// architecture — empty means the whole organization
	RepositoryIDs []string `json:"repository_ids,omitempty"`
	// glossary — empty means every term in the organization
	TermIDs []string `json:"term_ids,omitempty"`
	// contacts
	People []OnboardingStepContact `json:"people,omitempty"`
	// checklist
	Items []OnboardingStepChecklistItem `json:"items,omitempty"`
	// link
	URL   string `json:"url,omitempty"`
	Label string `json:"label,omitempty"`
	// task
	Instructions string `json:"instructions,omitempty"`
	TaskURL      string `json:"task_url,omitempty"`
	// verified
	Check OnboardingVerifiedCheck `json:"check,omitempty"`
}

// OnboardingStep is one screen of a flow.
type OnboardingStep struct {
	ID     string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	FlowID string `gorm:"type:uuid;not null;index" json:"flow_id"`

	// Position is rewritten from array order every time the step list is saved,
	// so callers never manage it by hand.
	Position int                `gorm:"not null" json:"position"`
	Kind     OnboardingStepKind `gorm:"type:varchar(50);not null" json:"kind"`
	Title    string             `gorm:"type:varchar(255);not null" json:"title"`

	// Body is markdown, and only the editorial kinds use it.
	Body string `gorm:"type:text" json:"body,omitempty"`

	Config datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"config"`

	IsRequired bool `gorm:"not null;default:true" json:"is_required"`
	// EstimatedMinutes is nil when unstated, which is not the same as zero.
	EstimatedMinutes *int `gorm:"column:estimated_minutes" json:"estimated_minutes,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (OnboardingStep) TableName() string {
	return "onboarding_steps"
}

// DecodeConfig unmarshals the JSONB blob. An empty blob decodes to the zero
// config, so a step written before a field existed does not error.
func (s *OnboardingStep) DecodeConfig() (OnboardingStepConfig, error) {
	var cfg OnboardingStepConfig
	raw := strings.TrimSpace(string(s.Config))
	if raw == "" || raw == "null" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("step %q: decode config: %w", s.Title, err)
	}
	return cfg, nil
}

// SetConfig marshals cfg onto the step.
func (s *OnboardingStep) SetConfig(cfg OnboardingStepConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode step config: %w", err)
	}
	s.Config = data
	return nil
}

// Validate checks the step's shape: that the kind is known, and that the kind
// has what it needs to render.
//
// It deliberately does not check that referenced ids exist or belong to the
// caller's organization — that needs the database, and lives in the service.
// References() is what feeds it.
func (s *OnboardingStep) Validate() error {
	if strings.TrimSpace(s.Title) == "" {
		return fmt.Errorf("step title is required")
	}
	if !s.Kind.IsKnown() {
		return fmt.Errorf("unknown step kind %q", s.Kind)
	}
	if s.EstimatedMinutes != nil && *s.EstimatedMinutes < 0 {
		return fmt.Errorf("step %q: estimated_minutes cannot be negative", s.Title)
	}

	cfg, err := s.DecodeConfig()
	if err != nil {
		return err
	}

	switch s.Kind {
	case OnboardingStepKindMarkdown:
		if strings.TrimSpace(s.Body) == "" {
			return fmt.Errorf("step %q: markdown steps need a body", s.Title)
		}
	case OnboardingStepKindRepository:
		if cfg.RepositoryID == "" {
			return fmt.Errorf("step %q: repository steps need repository_id", s.Title)
		}
	case OnboardingStepKindTeam:
		if cfg.TeamID == "" {
			return fmt.Errorf("step %q: team steps need team_id", s.Title)
		}
	case OnboardingStepKindDoc:
		if cfg.DocGenerationID == "" {
			return fmt.Errorf("step %q: doc steps need doc_generation_id", s.Title)
		}
	case OnboardingStepKindLink:
		if cfg.URL == "" {
			return fmt.Errorf("step %q: link steps need a url", s.Title)
		}
	case OnboardingStepKindChecklist:
		if len(cfg.Items) == 0 {
			return fmt.Errorf("step %q: checklist steps need at least one item", s.Title)
		}
		for i := range cfg.Items {
			if strings.TrimSpace(cfg.Items[i].Text) == "" {
				return fmt.Errorf("step %q: checklist item %d has no text", s.Title, i+1)
			}
		}
	case OnboardingStepKindContacts:
		if len(cfg.People) == 0 {
			return fmt.Errorf("step %q: contact steps need at least one person", s.Title)
		}
		for i := range cfg.People {
			if cfg.People[i].UserID == "" {
				return fmt.Errorf("step %q: contact %d has no user", s.Title, i+1)
			}
		}
	case OnboardingStepKindTask:
		// A task with neither a link nor instructions tells the person nothing.
		if strings.TrimSpace(cfg.Instructions) == "" && cfg.TaskURL == "" {
			return fmt.Errorf("step %q: task steps need instructions or a url", s.Title)
		}
	case OnboardingStepKindVerified:
		switch cfg.Check {
		case OnboardingCheckFirstChangeRequest:
			if cfg.RepositoryID == "" {
				return fmt.Errorf("step %q: the first change request check needs repository_id", s.Title)
			}
		case OnboardingCheckTeamMembership:
			// team_id is optional: empty means "any team".
		default:
			return fmt.Errorf("step %q: unknown verification %q", s.Title, cfg.Check)
		}
	case OnboardingStepKindArchitecture, OnboardingStepKindGlossary:
		// Both accept an empty selection, meaning "everything in the org".
	}

	return nil
}

// OnboardingStepReferences are the entities a step points at.
//
// The service checks these against the caller's organization in one generic
// pass, instead of repeating a per-kind switch next to every write.
type OnboardingStepReferences struct {
	RepositoryIDs    []string
	TeamIDs          []string
	DocGenerationIDs []string
	GlossaryTermIDs  []string
	UserIDs          []string
}

// References returns every id the step depends on.
func (s *OnboardingStep) References() (OnboardingStepReferences, error) {
	var refs OnboardingStepReferences
	cfg, err := s.DecodeConfig()
	if err != nil {
		return refs, err
	}

	if cfg.RepositoryID != "" {
		refs.RepositoryIDs = append(refs.RepositoryIDs, cfg.RepositoryID)
	}
	refs.RepositoryIDs = append(refs.RepositoryIDs, cfg.RepositoryIDs...)
	if cfg.TeamID != "" {
		refs.TeamIDs = append(refs.TeamIDs, cfg.TeamID)
	}
	if cfg.DocGenerationID != "" {
		refs.DocGenerationIDs = append(refs.DocGenerationIDs, cfg.DocGenerationID)
	}
	refs.GlossaryTermIDs = append(refs.GlossaryTermIDs, cfg.TermIDs...)
	for i := range cfg.People {
		if cfg.People[i].UserID != "" {
			refs.UserIDs = append(refs.UserIDs, cfg.People[i].UserID)
		}
	}
	return refs, nil
}

// OnboardingAssignmentStatus tracks one person's walk through one flow.
type OnboardingAssignmentStatus string

const (
	OnboardingAssignmentPending    OnboardingAssignmentStatus = "pending"
	OnboardingAssignmentInProgress OnboardingAssignmentStatus = "in_progress"
	OnboardingAssignmentCompleted  OnboardingAssignmentStatus = "completed"
	// OnboardingAssignmentAbandoned frees the (flow, user) pair so the same flow
	// can be assigned again, without deleting the history of the first attempt.
	OnboardingAssignmentAbandoned OnboardingAssignmentStatus = "abandoned"
)

// OnboardingAssignment is one person's walk through one flow.
//
// It is a row rather than a flag on the membership because there are many
// flows, because changing teams is a second onboarding, and because the
// progress rows need a parent.
type OnboardingAssignment struct {
	ID             string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrganizationID string `gorm:"type:uuid;not null;index" json:"organization_id"`
	FlowID         string `gorm:"type:uuid;not null;index" json:"flow_id"`
	UserID         string `gorm:"type:uuid;not null;index" json:"user_id"`

	AssignedByUserID *string `gorm:"type:uuid" json:"assigned_by_user_id,omitempty"`
	// InviteID records that this assignment came from an invite, for the audit
	// trail of who let whom in with which onboarding.
	InviteID *string `gorm:"type:uuid" json:"invite_id,omitempty"`

	Status OnboardingAssignmentStatus `gorm:"type:varchar(20);not null;default:'pending'" json:"status"`

	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	Feedback   string     `gorm:"type:text" json:"feedback,omitempty"`
	FeedbackAt *time.Time `json:"feedback_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (OnboardingAssignment) TableName() string {
	return "onboarding_assignments"
}

// IsLive reports whether the assignment still counts — anything but abandoned.
// The partial unique index uses the same rule.
func (a *OnboardingAssignment) IsLive() bool {
	return a.Status != OnboardingAssignmentAbandoned
}

// MarkStarted moves a pending assignment to in_progress the first time the
// person touches it. Idempotent: an assignment already started keeps its
// original timestamp, which is what makes "time to complete" meaningful.
func (a *OnboardingAssignment) MarkStarted(now time.Time) {
	if a.Status == OnboardingAssignmentPending {
		a.Status = OnboardingAssignmentInProgress
	}
	if a.StartedAt == nil {
		started := now.UTC()
		a.StartedAt = &started
	}
	a.UpdatedAt = now.UTC()
}

// MarkCompleted closes the assignment.
func (a *OnboardingAssignment) MarkCompleted(now time.Time) {
	a.Status = OnboardingAssignmentCompleted
	completed := now.UTC()
	a.CompletedAt = &completed
	if a.StartedAt == nil {
		a.StartedAt = &completed
	}
	a.UpdatedAt = completed
}

// OnboardingStepStatus is the outcome recorded for a step. There is no
// "pending" value: a step with no row is pending, the same way a nil
// `metadata.has_ci` means "not verified" rather than "no CI".
type OnboardingStepStatus string

const (
	OnboardingStepDone    OnboardingStepStatus = "done"
	OnboardingStepSkipped OnboardingStepStatus = "skipped"
)

// IsKnown reports whether the status is one this build writes.
func (s OnboardingStepStatus) IsKnown() bool {
	return s == OnboardingStepDone || s == OnboardingStepSkipped
}

// OnboardingStepProgress is one person's outcome on one step.
type OnboardingStepProgress struct {
	AssignmentID string `gorm:"type:uuid;primaryKey" json:"assignment_id"`
	StepID       string `gorm:"type:uuid;primaryKey" json:"step_id"`

	Status OnboardingStepStatus `gorm:"type:varchar(20);not null" json:"status"`
	Note   string               `gorm:"type:text" json:"note,omitempty"`

	CompletedAt time.Time `json:"completed_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (OnboardingStepProgress) TableName() string {
	return "onboarding_step_progress"
}
