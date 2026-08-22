package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/onboarding"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

var (
	ErrOnboardingFlowNotFound     = errors.New("onboarding flow not found")
	ErrOnboardingFlowInvalid      = errors.New("onboarding flow name is required")
	ErrOnboardingFlowSlugTaken    = errors.New("an onboarding flow with this name already exists in the organization")
	ErrOnboardingTemplateNotFound = errors.New("onboarding template not found")

	// ErrOnboardingStepInvalid wraps the shape problem the model reported. The
	// message names the step, so a builder with twenty steps says which one.
	ErrOnboardingStepInvalid = errors.New("invalid onboarding step")
	// ErrOnboardingReferenceNotInOrganization is the cross-organization guard: a
	// step must never point at a repository, team, document, term or person from
	// somewhere else, or walking the flow would leak them.
	ErrOnboardingReferenceNotInOrganization = errors.New("that reference does not belong to this organization")

	ErrGlossaryTermNotFound = errors.New("glossary term not found")
	ErrGlossaryTermInvalid  = errors.New("term and definition are required")
	ErrGlossaryTermTaken    = errors.New("that term already exists in the organization")
)

var onboardingSlugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

type OnboardingService struct {
	repo storage.Repository
}

func NewOnboardingService(repo storage.Repository) *OnboardingService {
	return &OnboardingService{repo: repo}
}

// ── flows ────────────────────────────────────────────────────────────────────

// getScopedFlow reads a flow and refuses one from another organization. Like
// the team service, it reports "not found" rather than "forbidden": the caller
// has no business learning that the id exists elsewhere.
func (s *OnboardingService) getScopedFlow(ctx context.Context, orgID, flowID string) (*models.OnboardingFlow, error) {
	flow, err := s.repo.GetOnboardingFlow(ctx, flowID)
	if err != nil {
		return nil, err
	}
	if flow == nil || flow.OrganizationID != orgID {
		return nil, ErrOnboardingFlowNotFound
	}
	return flow, nil
}

func (s *OnboardingService) CreateFlow(ctx context.Context, orgID, actorUserID string, req models.CreateOnboardingFlowRequest) (*models.OnboardingFlow, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrOnboardingFlowInvalid
	}

	slug := slugifyOnboarding(req.Slug)
	if slug == "" {
		slug = slugifyOnboarding(name)
	}
	if slug == "" {
		return nil, ErrOnboardingFlowInvalid
	}

	existing, err := s.repo.GetOnboardingFlowBySlug(ctx, orgID, slug)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrOnboardingFlowSlugTaken
	}

	var template *onboarding.Template
	if req.TemplateID != "" {
		if template = onboarding.TemplateByID(req.TemplateID); template == nil {
			return nil, ErrOnboardingTemplateNotFound
		}
	}

	flow := &models.OnboardingFlow{
		OrganizationID: orgID,
		Name:           name,
		Slug:           slug,
		Description:    strings.TrimSpace(req.Description),
		IsDefault:      req.IsDefault,
	}
	if actorUserID != "" {
		flow.CreatedByUserID = &actorUserID
	}

	// Demote the incumbent first: the partial unique index allows exactly one
	// default per organization, so promoting without demoting fails the write.
	if flow.IsDefault {
		if err := s.repo.ClearDefaultOnboardingFlow(ctx, orgID, ""); err != nil {
			return nil, err
		}
	}

	if err := s.repo.CreateOnboardingFlow(ctx, flow); err != nil {
		return nil, err
	}

	if template != nil {
		steps, err := stepsFromTemplate(template)
		if err != nil {
			return nil, err
		}
		if err := s.repo.ReplaceOnboardingSteps(ctx, flow.ID, steps); err != nil {
			return nil, err
		}
		flow.Steps = steps
	}

	return flow, nil
}

func (s *OnboardingService) ListFlows(ctx context.Context, orgID string) ([]models.OnboardingFlow, map[string]int, error) {
	flows, err := s.repo.ListOnboardingFlows(ctx, orgID)
	if err != nil {
		return nil, nil, err
	}
	if len(flows) == 0 {
		return flows, map[string]int{}, nil
	}

	ids := make([]string, 0, len(flows))
	for i := range flows {
		ids = append(ids, flows[i].ID)
	}
	counts, err := s.repo.CountOnboardingStepsByFlow(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	return flows, counts, nil
}

// GetFlow returns a flow with its steps, which is what both the builder and the
// runner need.
func (s *OnboardingService) GetFlow(ctx context.Context, orgID, flowID string) (*models.OnboardingFlow, error) {
	flow, err := s.getScopedFlow(ctx, orgID, flowID)
	if err != nil {
		return nil, err
	}
	steps, err := s.repo.ListOnboardingSteps(ctx, flow.ID)
	if err != nil {
		return nil, err
	}
	flow.Steps = steps
	return flow, nil
}

func (s *OnboardingService) UpdateFlow(ctx context.Context, orgID, flowID string, req models.UpdateOnboardingFlowRequest) (*models.OnboardingFlow, error) {
	flow, err := s.getScopedFlow(ctx, orgID, flowID)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ErrOnboardingFlowInvalid
		}
		flow.Name = name
	}
	if req.Slug != nil {
		slug := slugifyOnboarding(*req.Slug)
		if slug == "" {
			return nil, ErrOnboardingFlowInvalid
		}
		if slug != flow.Slug {
			taken, err := s.repo.GetOnboardingFlowBySlug(ctx, orgID, slug)
			if err != nil {
				return nil, err
			}
			if taken != nil {
				return nil, ErrOnboardingFlowSlugTaken
			}
		}
		flow.Slug = slug
	}
	if req.Description != nil {
		flow.Description = strings.TrimSpace(*req.Description)
	}
	if req.IsDefault != nil {
		if *req.IsDefault && !flow.IsDefault {
			if err := s.repo.ClearDefaultOnboardingFlow(ctx, orgID, flow.ID); err != nil {
				return nil, err
			}
		}
		flow.IsDefault = *req.IsDefault
	}

	if err := s.repo.UpdateOnboardingFlow(ctx, flow); err != nil {
		return nil, err
	}
	return flow, nil
}

func (s *OnboardingService) DeleteFlow(ctx context.Context, orgID, flowID string) error {
	flow, err := s.getScopedFlow(ctx, orgID, flowID)
	if err != nil {
		return err
	}
	return s.repo.DeleteOnboardingFlow(ctx, flow.ID)
}

// DuplicateFlow copies a flow and its steps. With several flows that differ in
// a step or two, duplicating is what people actually do, and doing it by hand
// in the builder is where steps get dropped.
//
// The copy is never the default and its steps get fresh ids, so the original's
// progress rows are untouched.
func (s *OnboardingService) DuplicateFlow(ctx context.Context, orgID, actorUserID, flowID string) (*models.OnboardingFlow, error) {
	source, err := s.GetFlow(ctx, orgID, flowID)
	if err != nil {
		return nil, err
	}

	name, slug, err := s.availableCopyName(ctx, orgID, source.Name)
	if err != nil {
		return nil, err
	}

	copyFlow := &models.OnboardingFlow{
		OrganizationID: orgID,
		Name:           name,
		Slug:           slug,
		Description:    source.Description,
		IsDefault:      false,
	}
	if actorUserID != "" {
		copyFlow.CreatedByUserID = &actorUserID
	}
	if err := s.repo.CreateOnboardingFlow(ctx, copyFlow); err != nil {
		return nil, err
	}

	steps := make([]models.OnboardingStep, 0, len(source.Steps))
	for i := range source.Steps {
		step := source.Steps[i]
		// A fresh id: sharing one would make the copy's progress collide with
		// the original's.
		step.ID = ""
		step.FlowID = copyFlow.ID
		steps = append(steps, step)
	}
	if len(steps) > 0 {
		if err := s.repo.ReplaceOnboardingSteps(ctx, copyFlow.ID, steps); err != nil {
			return nil, err
		}
	}
	copyFlow.Steps = steps

	return copyFlow, nil
}

// availableCopyName finds a free "(cópia)" name, numbering after the first.
func (s *OnboardingService) availableCopyName(ctx context.Context, orgID, sourceName string) (string, string, error) {
	for attempt := 0; attempt < 50; attempt++ {
		name := sourceName + " (cópia)"
		if attempt > 0 {
			name = fmt.Sprintf("%s (cópia %d)", sourceName, attempt+1)
		}
		slug := slugifyOnboarding(name)
		existing, err := s.repo.GetOnboardingFlowBySlug(ctx, orgID, slug)
		if err != nil {
			return "", "", err
		}
		if existing == nil {
			return name, slug, nil
		}
	}
	return "", "", ErrOnboardingFlowSlugTaken
}

// ── steps ────────────────────────────────────────────────────────────────────

// ReplaceSteps validates and saves the whole step list.
//
// Validation is in two halves on purpose: the model checks each step's shape
// (does a repository step name a repository at all), and this checks that every
// referenced entity actually belongs to the caller's organization. The second
// half needs the database, and it runs before any write so a bad list is
// rejected whole rather than half-applied.
func (s *OnboardingService) ReplaceSteps(ctx context.Context, orgID, flowID string, inputs []models.OnboardingStepInput) ([]models.OnboardingStep, error) {
	flow, err := s.getScopedFlow(ctx, orgID, flowID)
	if err != nil {
		return nil, err
	}

	steps := make([]models.OnboardingStep, 0, len(inputs))
	for i := range inputs {
		input := inputs[i]
		step := models.OnboardingStep{
			ID:     strings.TrimSpace(input.ID),
			FlowID: flow.ID,
			// Array order is the flow's order. Storage rewrites this too — it is
			// the authority on what gets persisted — but setting it here means
			// the value returned to the caller is right without depending on
			// storage mutating the slice it was handed.
			Position:         i,
			Kind:             input.Kind,
			Title:            strings.TrimSpace(input.Title),
			Body:             input.Body,
			IsRequired:       true,
			EstimatedMinutes: input.EstimatedMinutes,
		}
		if input.IsRequired != nil {
			step.IsRequired = *input.IsRequired
		}
		if err := step.SetConfig(input.Config); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrOnboardingStepInvalid, err)
		}
		if err := step.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrOnboardingStepInvalid, err)
		}
		steps = append(steps, step)
	}

	if err := s.validateReferences(ctx, orgID, steps); err != nil {
		return nil, err
	}

	if err := s.repo.ReplaceOnboardingSteps(ctx, flow.ID, steps); err != nil {
		return nil, err
	}
	return steps, nil
}

// validateReferences checks every entity every step points at, deduplicated so
// a flow that mentions the same repository ten times costs one query.
func (s *OnboardingService) validateReferences(ctx context.Context, orgID string, steps []models.OnboardingStep) error {
	repos := map[string]bool{}
	teams := map[string]bool{}
	docs := map[string]bool{}
	terms := map[string]bool{}
	users := map[string]bool{}

	for i := range steps {
		refs, err := steps[i].References()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrOnboardingStepInvalid, err)
		}
		for _, id := range refs.RepositoryIDs {
			repos[id] = true
		}
		for _, id := range refs.TeamIDs {
			teams[id] = true
		}
		for _, id := range refs.DocGenerationIDs {
			docs[id] = true
		}
		for _, id := range refs.GlossaryTermIDs {
			terms[id] = true
		}
		for _, id := range refs.UserIDs {
			users[id] = true
		}
	}

	for id := range repos {
		repo, err := s.repo.GetRepository(ctx, id)
		if err != nil {
			return err
		}
		if repo == nil || repo.OrganizationID != orgID {
			return fmt.Errorf("%w: repository %s", ErrOnboardingReferenceNotInOrganization, id)
		}
	}
	for id := range teams {
		team, err := s.repo.GetTeam(ctx, id)
		if err != nil {
			return err
		}
		if team == nil || team.OrganizationID != orgID {
			return fmt.Errorf("%w: team %s", ErrOnboardingReferenceNotInOrganization, id)
		}
	}
	for id := range docs {
		doc, err := s.repo.GetDocGeneration(ctx, id)
		if err != nil {
			return err
		}
		if doc == nil || doc.OrganizationID != orgID {
			return fmt.Errorf("%w: document %s", ErrOnboardingReferenceNotInOrganization, id)
		}
	}
	for id := range terms {
		term, err := s.repo.GetGlossaryTerm(ctx, id)
		if err != nil {
			return err
		}
		if term == nil || term.OrganizationID != orgID {
			return fmt.Errorf("%w: term %s", ErrOnboardingReferenceNotInOrganization, id)
		}
	}
	for id := range users {
		// A contact has to be a member: naming someone outside the organization
		// would show a newcomer a person they cannot reach, and leak that the
		// account exists.
		member, err := s.repo.GetOrganizationMember(ctx, orgID, id)
		if err != nil {
			return err
		}
		if member == nil {
			return fmt.Errorf("%w: user %s", ErrOnboardingReferenceNotInOrganization, id)
		}
	}

	return nil
}

// stepsFromTemplate materializes a starter flow. Every seeded step is validated
// here too: a template is code, and code with a typo should fail loudly at the
// first create rather than store an unrenderable step.
func stepsFromTemplate(template *onboarding.Template) ([]models.OnboardingStep, error) {
	steps := make([]models.OnboardingStep, 0, len(template.Steps))
	for i := range template.Steps {
		seed := template.Steps[i]
		step := models.OnboardingStep{
			Kind:       models.OnboardingStepKind(seed.Kind),
			Title:      seed.Title,
			Body:       seed.Body,
			IsRequired: seed.IsRequired,
		}
		if seed.EstimatedMinutes > 0 {
			minutes := seed.EstimatedMinutes
			step.EstimatedMinutes = &minutes
		}
		if err := step.SetConfig(seed.Config); err != nil {
			return nil, fmt.Errorf("template %s: %w", template.ID, err)
		}
		if err := step.Validate(); err != nil {
			return nil, fmt.Errorf("template %s: %w", template.ID, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func slugifyOnboarding(raw string) string {
	slug := strings.ToLower(strings.TrimSpace(raw))
	slug = onboardingSlugInvalid.ReplaceAllString(slug, "-")
	return strings.Trim(slug, "-")
}

// ── glossary ─────────────────────────────────────────────────────────────────

func (s *OnboardingService) ListGlossaryTerms(ctx context.Context, orgID string) ([]models.GlossaryTerm, error) {
	return s.repo.ListGlossaryTerms(ctx, orgID)
}

func (s *OnboardingService) CreateGlossaryTerm(ctx context.Context, orgID, actorUserID string, req models.CreateGlossaryTermRequest) (*models.GlossaryTerm, error) {
	term := &models.GlossaryTerm{
		OrganizationID: orgID,
		Term:           strings.TrimSpace(req.Term),
		Definition:     strings.TrimSpace(req.Definition),
	}
	if actorUserID != "" {
		term.CreatedByUserID = &actorUserID
	}
	if err := term.Validate(); err != nil {
		return nil, ErrGlossaryTermInvalid
	}

	duplicate, err := s.findTermByName(ctx, orgID, term.Term, "")
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		return nil, ErrGlossaryTermTaken
	}

	if err := s.repo.CreateGlossaryTerm(ctx, term); err != nil {
		return nil, err
	}
	return term, nil
}

func (s *OnboardingService) UpdateGlossaryTerm(ctx context.Context, orgID, termID string, req models.UpdateGlossaryTermRequest) (*models.GlossaryTerm, error) {
	term, err := s.repo.GetGlossaryTerm(ctx, termID)
	if err != nil {
		return nil, err
	}
	if term == nil || term.OrganizationID != orgID {
		return nil, ErrGlossaryTermNotFound
	}

	if req.Term != nil {
		term.Term = strings.TrimSpace(*req.Term)
	}
	if req.Definition != nil {
		term.Definition = strings.TrimSpace(*req.Definition)
	}
	if err := term.Validate(); err != nil {
		return nil, ErrGlossaryTermInvalid
	}

	duplicate, err := s.findTermByName(ctx, orgID, term.Term, term.ID)
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		return nil, ErrGlossaryTermTaken
	}

	if err := s.repo.UpdateGlossaryTerm(ctx, term); err != nil {
		return nil, err
	}
	return term, nil
}

func (s *OnboardingService) DeleteGlossaryTerm(ctx context.Context, orgID, termID string) error {
	term, err := s.repo.GetGlossaryTerm(ctx, termID)
	if err != nil {
		return err
	}
	if term == nil || term.OrganizationID != orgID {
		return ErrGlossaryTermNotFound
	}
	return s.repo.DeleteGlossaryTerm(ctx, term.ID)
}

// findTermByName reports a case-insensitive clash, matching the partial unique
// index — catching it here turns a database error into a clear message.
func (s *OnboardingService) findTermByName(ctx context.Context, orgID, name, exceptID string) (*models.GlossaryTerm, error) {
	terms, err := s.repo.ListGlossaryTerms(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range terms {
		if terms[i].ID == exceptID {
			continue
		}
		if strings.EqualFold(terms[i].Term, name) {
			return &terms[i], nil
		}
	}
	return nil, nil
}

// Templates exposes the starter registry, so the handler does not import the
// onboarding package directly.
func (s *OnboardingService) Templates() []onboarding.Template {
	return onboarding.Templates()
}
