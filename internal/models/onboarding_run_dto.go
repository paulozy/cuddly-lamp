package models

import "time"

// The types here are what the runner renders. A step arrives with the live
// entity it points at already resolved — the repository with its scorecard, the
// team with its members and the repositories it answers for, the document's
// markdown — so the client makes one request instead of one per step, and the
// authorization for all of it stays on the server.

// OnboardingResolvedTeam answers "who is this team, and what do they answer
// for", which is the whole reason a team step exists.
type OnboardingResolvedTeam struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Slug         string               `json:"slug"`
	Description  string               `json:"description,omitempty"`
	Members      []TeamMemberResponse `json:"members"`
	Repositories []TeamRepositoryRef  `json:"repositories"`
}

// TeamRepositoryRef is one repository a team answers for, trimmed to what a
// step needs to show.
type TeamRepositoryRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// OnboardingResolvedDoc is generated documentation rendered inside a step.
type OnboardingResolvedDoc struct {
	ID string `json:"id"`
	// DocType is the section rendered, when the generation produced several.
	DocType   string    `json:"doc_type,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// OnboardingResolvedContact is a person to talk to: a live member, plus the
// editorial note about when to reach for them.
type OnboardingResolvedContact struct {
	UserID      string `json:"user_id"`
	FullName    string `json:"full_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Area        string `json:"area,omitempty"`
	WhenToReach string `json:"when_to_reach,omitempty"`
}

// OnboardingVerificationState describes a verified step: what is checked, and
// what the last check found.
type OnboardingVerificationState struct {
	Check OnboardingVerifiedCheck `json:"check"`
	// Description is the plain-language claim being made, so the UI can show
	// what is being proven instead of a bare tick.
	Description string `json:"description"`
}

// OnboardingVerificationResult is the outcome of running a check.
type OnboardingVerificationResult struct {
	Passed bool `json:"passed"`
	// Pending means the check could not run — nobody has connected the provider
	// yet, or the organization has no token for it. It is not a failure, and
	// saying "not yet confirmed" is the honest rendering.
	Pending bool `json:"pending"`
	// How states what was actually inspected. A verification that cannot say
	// how it verified is indistinguishable from a checkbox.
	How    string `json:"how"`
	Detail string `json:"detail,omitempty"`
}

// OnboardingStepResolved carries whichever live entity the step's kind points
// at. Exactly one field is populated, chosen by kind.
type OnboardingStepResolved struct {
	Repository   *RepositoryResponse          `json:"repository,omitempty"`
	Team         *OnboardingResolvedTeam      `json:"team,omitempty"`
	Doc          *OnboardingResolvedDoc       `json:"doc,omitempty"`
	Graph        *RepositoryGraphResponse     `json:"graph,omitempty"`
	Terms        []GlossaryTermResponse       `json:"terms,omitempty"`
	People       []OnboardingResolvedContact  `json:"people,omitempty"`
	Verification *OnboardingVerificationState `json:"verification,omitempty"`
}

// OnboardingRunStep is a step as the person walking the flow sees it.
type OnboardingRunStep struct {
	OnboardingStepResponse

	// Status is empty when the step is still pending: the absence of a
	// progress row, not a third stored value.
	Status      OnboardingStepStatus `json:"status,omitempty"`
	Note        string               `json:"note,omitempty"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`

	Resolved *OnboardingStepResolved `json:"resolved,omitempty"`
	// Unavailable explains, in place of the entity, why it could not be shown —
	// a repository that was deleted, a document that was removed. A step whose
	// reference vanished still renders; it just says so.
	Unavailable string `json:"unavailable,omitempty"`
}

// OnboardingRunResponse is the whole walk: the assignment, its flow, and every
// step with its progress.
type OnboardingRunResponse struct {
	AssignmentID string                     `json:"assignment_id"`
	Status       OnboardingAssignmentStatus `json:"status"`
	FlowID       string                     `json:"flow_id"`
	FlowName     string                     `json:"flow_name"`
	FlowSummary  string                     `json:"flow_summary,omitempty"`

	StepsTotal int `json:"steps_total"`
	StepsDone  int `json:"steps_done"`
	// RequiredRemaining counts required steps still pending, which is what
	// decides whether the flow can be completed and whether the banner shows.
	RequiredRemaining int `json:"required_remaining"`
	TotalMinutes      int `json:"total_minutes"`

	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Feedback    string     `json:"feedback,omitempty"`

	Steps []OnboardingRunStep `json:"steps"`
}

// OnboardingRunListResponse is what the runner asks for on load: every live
// assignment the caller has. Usually one, occasionally two when someone
// changed teams.
type OnboardingRunListResponse struct {
	Items []OnboardingRunResponse `json:"items"`
	Total int                     `json:"total"`
}

// MarkOnboardingStepRequest records an outcome for one step.
type MarkOnboardingStepRequest struct {
	// Status is `done` or `skipped`.
	Status OnboardingStepStatus `json:"status" binding:"required"`
	Note   string               `json:"note,omitempty"`
}

// OnboardingFeedbackRequest is the answer to "what was missing?".
type OnboardingFeedbackRequest struct {
	Feedback string `json:"feedback" binding:"required"`
}
