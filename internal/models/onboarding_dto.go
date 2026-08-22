package models

import "time"

// OnboardingStepResponse is one step as the API renders it.
//
// Config is returned decoded rather than as a raw JSON blob, so the contract is
// documented and the frontend does not parse twice. CompletionMode is derived
// from the kind and included deliberately: it is what tells the UI whether to
// say "marked by you" or "verified", and duplicating that rule in the client is
// how a UI ends up claiming a verification that never happened.
type OnboardingStepResponse struct {
	ID               string                   `json:"id"`
	Position         int                      `json:"position"`
	Kind             OnboardingStepKind       `json:"kind"`
	Title            string                   `json:"title"`
	Body             string                   `json:"body,omitempty"`
	Config           OnboardingStepConfig     `json:"config"`
	IsRequired       bool                     `json:"is_required"`
	EstimatedMinutes *int                     `json:"estimated_minutes,omitempty"`
	CompletionMode   OnboardingCompletionMode `json:"completion_mode"`
}

// OnboardingFlowResponse is a flow, with its steps when they were loaded.
type OnboardingFlowResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"is_default"`
	StepCount   int    `json:"step_count"`
	// TotalMinutes sums the steps that state an estimate, so the list can say
	// "about 40 minutes" without the client adding it up.
	TotalMinutes int                      `json:"total_minutes"`
	Steps        []OnboardingStepResponse `json:"steps,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

type OnboardingFlowListResponse struct {
	Items []OnboardingFlowResponse `json:"items"`
	Total int                      `json:"total"`
}

type CreateOnboardingFlowRequest struct {
	Name        string `json:"name" binding:"required"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
	// TemplateID seeds the flow from the static registry in
	// `internal/onboarding`. Empty creates an empty flow.
	TemplateID string `json:"template_id"`
}

type UpdateOnboardingFlowRequest struct {
	Name        *string `json:"name"`
	Slug        *string `json:"slug"`
	Description *string `json:"description"`
	IsDefault   *bool   `json:"is_default"`
}

// OnboardingStepInput is one step in a save. An empty ID means a new step; a
// present ID must already belong to the flow, which is what preserves the
// progress rows pointing at it.
type OnboardingStepInput struct {
	ID               string               `json:"id"`
	Kind             OnboardingStepKind   `json:"kind" binding:"required"`
	Title            string               `json:"title" binding:"required"`
	Body             string               `json:"body"`
	Config           OnboardingStepConfig `json:"config"`
	IsRequired       *bool                `json:"is_required"`
	EstimatedMinutes *int                 `json:"estimated_minutes"`
}

// ReplaceOnboardingStepsRequest carries the whole list. Order in the array is
// the order of the flow — position is derived from it, so there is no separate
// reorder call to keep consistent.
type ReplaceOnboardingStepsRequest struct {
	Steps []OnboardingStepInput `json:"steps"`
}

// OnboardingStepToResponse renders a step, decoding its config. A step whose
// config fails to decode still renders — with an empty config — because one
// corrupt row should degrade a single step rather than break the whole flow.
func OnboardingStepToResponse(step *OnboardingStep) OnboardingStepResponse {
	cfg, err := step.DecodeConfig()
	if err != nil {
		cfg = OnboardingStepConfig{}
	}
	return OnboardingStepResponse{
		ID:               step.ID,
		Position:         step.Position,
		Kind:             step.Kind,
		Title:            step.Title,
		Body:             step.Body,
		Config:           cfg,
		IsRequired:       step.IsRequired,
		EstimatedMinutes: step.EstimatedMinutes,
		CompletionMode:   step.Kind.CompletionMode(step.IsRequired),
	}
}

// OnboardingFlowToResponse renders a flow. stepCount is passed in because the
// list endpoint counts steps in one grouped query rather than loading them.
func OnboardingFlowToResponse(flow *OnboardingFlow, stepCount int) OnboardingFlowResponse {
	resp := OnboardingFlowResponse{
		ID:          flow.ID,
		Name:        flow.Name,
		Slug:        flow.Slug,
		Description: flow.Description,
		IsDefault:   flow.IsDefault,
		StepCount:   stepCount,
		CreatedAt:   flow.CreatedAt,
		UpdatedAt:   flow.UpdatedAt,
	}

	if len(flow.Steps) > 0 {
		resp.Steps = make([]OnboardingStepResponse, 0, len(flow.Steps))
		for i := range flow.Steps {
			resp.Steps = append(resp.Steps, OnboardingStepToResponse(&flow.Steps[i]))
		}
		resp.StepCount = len(flow.Steps)
	}

	for i := range flow.Steps {
		if flow.Steps[i].EstimatedMinutes != nil {
			resp.TotalMinutes += *flow.Steps[i].EstimatedMinutes
		}
	}

	return resp
}

// ── glossary ─────────────────────────────────────────────────────────────────

type GlossaryTermResponse struct {
	ID         string    `json:"id"`
	Term       string    `json:"term"`
	Definition string    `json:"definition"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type GlossaryTermListResponse struct {
	Items []GlossaryTermResponse `json:"items"`
	Total int                    `json:"total"`
}

type CreateGlossaryTermRequest struct {
	Term       string `json:"term" binding:"required"`
	Definition string `json:"definition" binding:"required"`
}

type UpdateGlossaryTermRequest struct {
	Term       *string `json:"term"`
	Definition *string `json:"definition"`
}

func GlossaryTermToResponse(term *GlossaryTerm) GlossaryTermResponse {
	return GlossaryTermResponse{
		ID:         term.ID,
		Term:       term.Term,
		Definition: term.Definition,
		CreatedAt:  term.CreatedAt,
		UpdatedAt:  term.UpdatedAt,
	}
}
