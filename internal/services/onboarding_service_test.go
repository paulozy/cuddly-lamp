package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/onboarding"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// ── mock store ───────────────────────────────────────────────────────────────

type mockOnboardingStore struct {
	storage.Repository

	flows map[string]*models.OnboardingFlow
	steps map[string][]models.OnboardingStep
	terms map[string]*models.GlossaryTerm

	// The entities steps can point at, each keyed by id with the organization
	// it belongs to — enough to exercise the cross-organization guard.
	repos   map[string]string
	teams   map[string]string
	docs    map[string]string
	members map[string]string // "orgID/userID"

	nextID int
}

func newMockOnboardingStore() *mockOnboardingStore {
	return &mockOnboardingStore{
		flows:   map[string]*models.OnboardingFlow{},
		steps:   map[string][]models.OnboardingStep{},
		terms:   map[string]*models.GlossaryTerm{},
		repos:   map[string]string{},
		teams:   map[string]string{},
		docs:    map[string]string{},
		members: map[string]string{},
	}
}

func (m *mockOnboardingStore) id(prefix string) string {
	m.nextID++
	return prefix + "-" + string(rune('a'+m.nextID-1))
}

func (m *mockOnboardingStore) CreateOnboardingFlow(_ context.Context, flow *models.OnboardingFlow) error {
	if flow.ID == "" {
		flow.ID = m.id("flow")
	}
	copyFlow := *flow
	m.flows[flow.ID] = &copyFlow
	return nil
}

func (m *mockOnboardingStore) GetOnboardingFlow(_ context.Context, id string) (*models.OnboardingFlow, error) {
	flow, ok := m.flows[id]
	if !ok || flow.DeletedAt != nil {
		return nil, nil
	}
	copyFlow := *flow
	return &copyFlow, nil
}

func (m *mockOnboardingStore) GetOnboardingFlowBySlug(_ context.Context, orgID, slug string) (*models.OnboardingFlow, error) {
	for _, flow := range m.flows {
		if flow.OrganizationID == orgID && flow.Slug == slug && flow.DeletedAt == nil {
			copyFlow := *flow
			return &copyFlow, nil
		}
	}
	return nil, nil
}

func (m *mockOnboardingStore) GetDefaultOnboardingFlow(_ context.Context, orgID string) (*models.OnboardingFlow, error) {
	for _, flow := range m.flows {
		if flow.OrganizationID == orgID && flow.IsDefault && flow.DeletedAt == nil {
			copyFlow := *flow
			return &copyFlow, nil
		}
	}
	return nil, nil
}

func (m *mockOnboardingStore) ListOnboardingFlows(_ context.Context, orgID string) ([]models.OnboardingFlow, error) {
	var out []models.OnboardingFlow
	for _, flow := range m.flows {
		if flow.OrganizationID == orgID && flow.DeletedAt == nil {
			out = append(out, *flow)
		}
	}
	return out, nil
}

func (m *mockOnboardingStore) UpdateOnboardingFlow(_ context.Context, flow *models.OnboardingFlow) error {
	copyFlow := *flow
	m.flows[flow.ID] = &copyFlow
	return nil
}

func (m *mockOnboardingStore) ClearDefaultOnboardingFlow(_ context.Context, orgID, exceptFlowID string) error {
	for id, flow := range m.flows {
		if flow.OrganizationID == orgID && id != exceptFlowID {
			flow.IsDefault = false
		}
	}
	return nil
}

func (m *mockOnboardingStore) DeleteOnboardingFlow(_ context.Context, id string) error {
	if flow, ok := m.flows[id]; ok {
		now := time.Now().UTC()
		flow.DeletedAt = &now
		flow.IsDefault = false
	}
	return nil
}

func (m *mockOnboardingStore) ListOnboardingSteps(_ context.Context, flowID string) ([]models.OnboardingStep, error) {
	return append([]models.OnboardingStep(nil), m.steps[flowID]...), nil
}

func (m *mockOnboardingStore) CountOnboardingStepsByFlow(_ context.Context, flowIDs []string) (map[string]int, error) {
	counts := map[string]int{}
	for _, id := range flowIDs {
		counts[id] = len(m.steps[id])
	}
	return counts, nil
}

// Mirrors the Postgres implementation, which mutates the caller's slice through
// the index: ids are assigned to new steps and positions come from order.
func (m *mockOnboardingStore) ReplaceOnboardingSteps(_ context.Context, flowID string, steps []models.OnboardingStep) error {
	stored := make([]models.OnboardingStep, 0, len(steps))
	for i := range steps {
		step := &steps[i]
		if step.ID == "" {
			step.ID = m.id("step")
		}
		step.Position = i
		step.FlowID = flowID
		stored = append(stored, *step)
	}
	m.steps[flowID] = stored
	return nil
}

func (m *mockOnboardingStore) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	orgID, ok := m.repos[id]
	if !ok {
		return nil, nil
	}
	return &models.Repository{ID: id, OrganizationID: orgID}, nil
}

func (m *mockOnboardingStore) GetTeam(_ context.Context, id string) (*models.Team, error) {
	orgID, ok := m.teams[id]
	if !ok {
		return nil, nil
	}
	return &models.Team{ID: id, OrganizationID: orgID}, nil
}

func (m *mockOnboardingStore) GetDocGeneration(_ context.Context, id string) (*models.DocGeneration, error) {
	orgID, ok := m.docs[id]
	if !ok {
		return nil, nil
	}
	return &models.DocGeneration{ID: id, OrganizationID: orgID}, nil
}

func (m *mockOnboardingStore) GetOrganizationMember(_ context.Context, orgID, userID string) (*models.OrganizationMember, error) {
	if m.members[orgID+"/"+userID] == "" {
		return nil, nil
	}
	return &models.OrganizationMember{OrganizationID: orgID, UserID: userID}, nil
}

func (m *mockOnboardingStore) CreateGlossaryTerm(_ context.Context, term *models.GlossaryTerm) error {
	if term.ID == "" {
		term.ID = m.id("term")
	}
	copyTerm := *term
	m.terms[term.ID] = &copyTerm
	return nil
}

func (m *mockOnboardingStore) GetGlossaryTerm(_ context.Context, id string) (*models.GlossaryTerm, error) {
	term, ok := m.terms[id]
	if !ok || term.DeletedAt != nil {
		return nil, nil
	}
	copyTerm := *term
	return &copyTerm, nil
}

func (m *mockOnboardingStore) ListGlossaryTerms(_ context.Context, orgID string) ([]models.GlossaryTerm, error) {
	var out []models.GlossaryTerm
	for _, term := range m.terms {
		if term.OrganizationID == orgID && term.DeletedAt == nil {
			out = append(out, *term)
		}
	}
	return out, nil
}

func (m *mockOnboardingStore) UpdateGlossaryTerm(_ context.Context, term *models.GlossaryTerm) error {
	copyTerm := *term
	m.terms[term.ID] = &copyTerm
	return nil
}

func (m *mockOnboardingStore) DeleteGlossaryTerm(_ context.Context, id string) error {
	if term, ok := m.terms[id]; ok {
		now := time.Now().UTC()
		term.DeletedAt = &now
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

const onbOrg = "org-1"

func stepInput(kind models.OnboardingStepKind, title string, cfg models.OnboardingStepConfig) models.OnboardingStepInput {
	return models.OnboardingStepInput{Kind: kind, Title: title, Config: cfg}
}

// ── flows ────────────────────────────────────────────────────────────────────

func TestOnboardingService_CreateFlow(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)

	flow, err := svc.CreateFlow(context.Background(), onbOrg, "user-1", models.CreateOnboardingFlowRequest{
		Name: "Dev Backend",
	})
	if err != nil {
		t.Fatalf("CreateFlow: %v", err)
	}
	if flow.Slug != "dev-backend" {
		t.Errorf("slug = %q, want it derived from the name", flow.Slug)
	}
	if flow.CreatedByUserID == nil || *flow.CreatedByUserID != "user-1" {
		t.Errorf("CreatedByUserID = %v, want the actor", flow.CreatedByUserID)
	}

	// Same name again collides on the derived slug.
	if _, err := svc.CreateFlow(context.Background(), onbOrg, "user-1", models.CreateOnboardingFlowRequest{
		Name: "Dev Backend",
	}); !errors.Is(err, ErrOnboardingFlowSlugTaken) {
		t.Fatalf("err = %v, want ErrOnboardingFlowSlugTaken", err)
	}

	// Another organization may use the same name.
	if _, err := svc.CreateFlow(context.Background(), "org-2", "user-2", models.CreateOnboardingFlowRequest{
		Name: "Dev Backend",
	}); err != nil {
		t.Fatalf("another organization should be free to reuse the name: %v", err)
	}
}

func TestOnboardingService_CreateFlow_RejectsBlankName(t *testing.T) {
	svc := NewOnboardingService(newMockOnboardingStore())

	for _, name := range []string{"", "   ", "///"} {
		_, err := svc.CreateFlow(context.Background(), onbOrg, "user-1", models.CreateOnboardingFlowRequest{Name: name})
		if !errors.Is(err, ErrOnboardingFlowInvalid) {
			t.Errorf("name %q: err = %v, want ErrOnboardingFlowInvalid", name, err)
		}
	}
}

func TestOnboardingService_CreateFlow_DemotesThePreviousDefault(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()

	first, err := svc.CreateFlow(ctx, onbOrg, "user-1", models.CreateOnboardingFlowRequest{Name: "Geral", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateFlow: %v", err)
	}
	second, err := svc.CreateFlow(ctx, onbOrg, "user-1", models.CreateOnboardingFlowRequest{Name: "Backend", IsDefault: true})
	if err != nil {
		t.Fatalf("CreateFlow: %v", err)
	}

	// The database allows exactly one default per organization, so promoting
	// has to demote — otherwise the write fails on the unique index.
	if store.flows[first.ID].IsDefault {
		t.Error("the previous default was not demoted")
	}
	if !store.flows[second.ID].IsDefault {
		t.Error("the new flow is not the default")
	}
}

func TestOnboardingService_UpdateFlow_PromotesDefault(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()

	first, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral", IsDefault: true})
	second, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Backend"})

	promote := true
	if _, err := svc.UpdateFlow(ctx, onbOrg, second.ID, models.UpdateOnboardingFlowRequest{IsDefault: &promote}); err != nil {
		t.Fatalf("UpdateFlow: %v", err)
	}

	if store.flows[first.ID].IsDefault {
		t.Error("the previous default was not demoted")
	}
	if !store.flows[second.ID].IsDefault {
		t.Error("the promoted flow is not the default")
	}
}

func TestOnboardingService_FlowsAreScopedToTheOrganization(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()

	flow, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral"})

	// Another organization must not read, edit or delete it — and gets "not
	// found" rather than "forbidden", so the id's existence stays private.
	if _, err := svc.GetFlow(ctx, "org-2", flow.ID); !errors.Is(err, ErrOnboardingFlowNotFound) {
		t.Errorf("GetFlow: err = %v, want ErrOnboardingFlowNotFound", err)
	}
	name := "Sequestrado"
	if _, err := svc.UpdateFlow(ctx, "org-2", flow.ID, models.UpdateOnboardingFlowRequest{Name: &name}); !errors.Is(err, ErrOnboardingFlowNotFound) {
		t.Errorf("UpdateFlow: err = %v, want ErrOnboardingFlowNotFound", err)
	}
	if err := svc.DeleteFlow(ctx, "org-2", flow.ID); !errors.Is(err, ErrOnboardingFlowNotFound) {
		t.Errorf("DeleteFlow: err = %v, want ErrOnboardingFlowNotFound", err)
	}
	if _, err := svc.ReplaceSteps(ctx, "org-2", flow.ID, nil); !errors.Is(err, ErrOnboardingFlowNotFound) {
		t.Errorf("ReplaceSteps: err = %v, want ErrOnboardingFlowNotFound", err)
	}
}

// ── templates ────────────────────────────────────────────────────────────────

// Every seeded step must satisfy the same validation a hand-written one does.
// A template is code, and this is the test that catches a typo in it before it
// stores a step the renderer cannot draw.
func TestOnboardingService_CreateFlow_EveryTemplateMaterializesValidSteps(t *testing.T) {
	for _, template := range onboarding.Templates() {
		t.Run(template.ID, func(t *testing.T) {
			store := newMockOnboardingStore()
			svc := NewOnboardingService(store)

			flow, err := svc.CreateFlow(context.Background(), onbOrg, "u", models.CreateOnboardingFlowRequest{
				Name:       "Fluxo " + template.ID,
				TemplateID: template.ID,
			})
			if err != nil {
				t.Fatalf("CreateFlow from template %s: %v", template.ID, err)
			}

			steps := store.steps[flow.ID]
			if len(steps) != len(template.Steps) {
				t.Fatalf("stored %d steps, want the template's %d", len(steps), len(template.Steps))
			}
			for i := range steps {
				if err := steps[i].Validate(); err != nil {
					t.Errorf("step %q: %v", steps[i].Title, err)
				}
				if steps[i].Position != i {
					t.Errorf("step %q: position = %d, want %d", steps[i].Title, steps[i].Position, i)
				}
			}
		})
	}
}

func TestOnboardingService_CreateFlow_UnknownTemplate(t *testing.T) {
	svc := NewOnboardingService(newMockOnboardingStore())
	_, err := svc.CreateFlow(context.Background(), onbOrg, "u", models.CreateOnboardingFlowRequest{
		Name:       "Fluxo",
		TemplateID: "does-not-exist",
	})
	if !errors.Is(err, ErrOnboardingTemplateNotFound) {
		t.Fatalf("err = %v, want ErrOnboardingTemplateNotFound", err)
	}
}

// ── steps ────────────────────────────────────────────────────────────────────

func TestOnboardingService_ReplaceSteps_SavesInArrayOrder(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral"})
	store.repos["repo-1"] = onbOrg

	steps, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Bem-vindo", Body: "# Oi"},
		stepInput(models.OnboardingStepKindRepository, "Nosso serviço", models.OnboardingStepConfig{RepositoryID: "repo-1"}),
		stepInput(models.OnboardingStepKindArchitecture, "O mapa", models.OnboardingStepConfig{}),
	})
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("saved %d steps, want 3", len(steps))
	}
	// Position comes from array order, which is what makes reordering a plain
	// save rather than a second endpoint.
	for i := range steps {
		if steps[i].Position != i {
			t.Errorf("step %d has position %d", i, steps[i].Position)
		}
	}
	// Required defaults to true when the payload omits it.
	if !steps[0].IsRequired {
		t.Error("IsRequired should default to true")
	}
}

func TestOnboardingService_ReplaceSteps_HonoursExplicitNotRequired(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral"})

	optional := false
	steps, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Extra", Body: "opcional", IsRequired: &optional},
	})
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	if steps[0].IsRequired {
		t.Error("an explicit false must be honoured, not overwritten by the default")
	}
}

func TestOnboardingService_ReplaceSteps_RejectsInvalidShape(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral"})

	_, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Bem-vindo", Body: "# Oi"},
		// A repository step naming no repository.
		stepInput(models.OnboardingStepKindRepository, "Serviço", models.OnboardingStepConfig{}),
	})
	if !errors.Is(err, ErrOnboardingStepInvalid) {
		t.Fatalf("err = %v, want ErrOnboardingStepInvalid", err)
	}
	// The whole list is rejected, not half-applied: the valid first step must
	// not have been stored either.
	if len(store.steps[flow.ID]) != 0 {
		t.Errorf("stored %d steps, want none", len(store.steps[flow.ID]))
	}
}

func TestOnboardingService_ReplaceSteps_RejectsForeignReferences(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral"})

	// Everything below exists — in somebody else's organization.
	store.repos["foreign-repo"] = "org-2"
	store.teams["foreign-team"] = "org-2"
	store.docs["foreign-doc"] = "org-2"
	store.terms["foreign-term"] = &models.GlossaryTerm{ID: "foreign-term", OrganizationID: "org-2", Term: "X", Definition: "y"}

	cases := []struct {
		name string
		step models.OnboardingStepInput
	}{
		{
			name: "repository",
			step: stepInput(models.OnboardingStepKindRepository, "Repo", models.OnboardingStepConfig{RepositoryID: "foreign-repo"}),
		},
		{
			name: "team",
			step: stepInput(models.OnboardingStepKindTeam, "Time", models.OnboardingStepConfig{TeamID: "foreign-team"}),
		},
		{
			name: "doc",
			step: stepInput(models.OnboardingStepKindDoc, "Doc", models.OnboardingStepConfig{DocGenerationID: "foreign-doc"}),
		},
		{
			name: "glossary term",
			step: stepInput(models.OnboardingStepKindGlossary, "Termos", models.OnboardingStepConfig{TermIDs: []string{"foreign-term"}}),
		},
		{
			name: "contact who is not a member",
			step: stepInput(models.OnboardingStepKindContacts, "Contatos", models.OnboardingStepConfig{
				People: []models.OnboardingStepContact{{UserID: "outsider", Area: "Acesso"}},
			}),
		},
		{
			name: "architecture over a foreign repository",
			step: stepInput(models.OnboardingStepKindArchitecture, "Mapa", models.OnboardingStepConfig{
				RepositoryIDs: []string{"foreign-repo"},
			}),
		},
		{
			name: "verification against a foreign repository",
			step: stepInput(models.OnboardingStepKindVerified, "Primeiro PR", models.OnboardingStepConfig{
				Check: models.OnboardingCheckFirstChangeRequest, RepositoryID: "foreign-repo",
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{tc.step})
			// Walking a flow renders whatever a step points at, so a reference
			// from another organization is a data leak, not a broken link.
			if !errors.Is(err, ErrOnboardingReferenceNotInOrganization) {
				t.Fatalf("err = %v, want ErrOnboardingReferenceNotInOrganization", err)
			}
			if len(store.steps[flow.ID]) != 0 {
				t.Error("a step with a foreign reference was stored")
			}
		})
	}
}

func TestOnboardingService_ReplaceSteps_AcceptsOwnReferences(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral"})

	store.repos["repo-1"] = onbOrg
	store.teams["team-1"] = onbOrg
	store.docs["doc-1"] = onbOrg
	store.terms["term-1"] = &models.GlossaryTerm{ID: "term-1", OrganizationID: onbOrg, Term: "SLO", Definition: "objetivo"}
	store.members[onbOrg+"/user-9"] = "member"

	_, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		stepInput(models.OnboardingStepKindRepository, "Repo", models.OnboardingStepConfig{RepositoryID: "repo-1"}),
		stepInput(models.OnboardingStepKindTeam, "Time", models.OnboardingStepConfig{TeamID: "team-1"}),
		stepInput(models.OnboardingStepKindDoc, "Doc", models.OnboardingStepConfig{DocGenerationID: "doc-1"}),
		stepInput(models.OnboardingStepKindGlossary, "Termos", models.OnboardingStepConfig{TermIDs: []string{"term-1"}}),
		stepInput(models.OnboardingStepKindContacts, "Contatos", models.OnboardingStepConfig{
			People: []models.OnboardingStepContact{{UserID: "user-9", Area: "Acesso", WhenToReach: "quando faltar permissão"}},
		}),
	})
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	if len(store.steps[flow.ID]) != 5 {
		t.Fatalf("stored %d steps, want 5", len(store.steps[flow.ID]))
	}
}

func TestOnboardingService_ReplaceSteps_KeepsExistingStepIDs(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Geral"})

	saved, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Primeiro", Body: "a"},
		{Kind: models.OnboardingStepKindMarkdown, Title: "Segundo", Body: "b"},
	})
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	firstID := saved[0].ID

	// Edit the title and swap the order, passing the ids back. Progress rows
	// point at those ids, so an edit must carry them through rather than mint
	// new ones.
	reordered, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{ID: saved[1].ID, Kind: models.OnboardingStepKindMarkdown, Title: "Segundo", Body: "b"},
		{ID: firstID, Kind: models.OnboardingStepKindMarkdown, Title: "Primeiro, corrigido", Body: "a"},
	})
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	if reordered[1].ID != firstID {
		t.Errorf("id = %q, want the original %q preserved", reordered[1].ID, firstID)
	}
	if reordered[1].Title != "Primeiro, corrigido" || reordered[1].Position != 1 {
		t.Errorf("step = %+v, want the edited title at position 1", reordered[1])
	}
}

// ── duplicate ────────────────────────────────────────────────────────────────

func TestOnboardingService_DuplicateFlow(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()

	source, _ := svc.CreateFlow(ctx, onbOrg, "u", models.CreateOnboardingFlowRequest{Name: "Backend", IsDefault: true})
	original, err := svc.ReplaceSteps(ctx, onbOrg, source.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Bem-vindo", Body: "# Oi"},
		{Kind: models.OnboardingStepKindMarkdown, Title: "Como trabalhamos", Body: "## Ritos"},
	})
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}

	copyFlow, err := svc.DuplicateFlow(ctx, onbOrg, "u2", source.ID)
	if err != nil {
		t.Fatalf("DuplicateFlow: %v", err)
	}

	if !strings.Contains(copyFlow.Name, "cópia") {
		t.Errorf("name = %q, want it marked as a copy", copyFlow.Name)
	}
	// Two defaults per organization is impossible, and silently stealing the
	// flag from the original would be worse than not copying it.
	if copyFlow.IsDefault {
		t.Error("the copy must not be the default")
	}
	if !store.flows[source.ID].IsDefault {
		t.Error("duplicating must not demote the original")
	}

	copies := store.steps[copyFlow.ID]
	if len(copies) != len(original) {
		t.Fatalf("copied %d steps, want %d", len(copies), len(original))
	}
	for i := range copies {
		if copies[i].ID == original[i].ID {
			// Sharing a step id would make progress on the copy collide with
			// progress on the original.
			t.Errorf("step %d kept the original id %q", i, copies[i].ID)
		}
		if copies[i].Title != original[i].Title {
			t.Errorf("step %d = %q, want %q", i, copies[i].Title, original[i].Title)
		}
	}

	// Duplicating twice must not collide on the slug.
	if _, err := svc.DuplicateFlow(ctx, onbOrg, "u2", source.ID); err != nil {
		t.Fatalf("second DuplicateFlow: %v", err)
	}
}

// ── glossary ─────────────────────────────────────────────────────────────────

func TestOnboardingService_GlossaryLifecycle(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()

	term, err := svc.CreateGlossaryTerm(ctx, onbOrg, "u", models.CreateGlossaryTermRequest{
		Term: "SLO", Definition: "Service Level Objective",
	})
	if err != nil {
		t.Fatalf("CreateGlossaryTerm: %v", err)
	}

	// Acronyms are the same term whatever the casing, which is what the partial
	// unique index enforces — caught here so the user gets a message instead of
	// a database error.
	if _, err := svc.CreateGlossaryTerm(ctx, onbOrg, "u", models.CreateGlossaryTermRequest{
		Term: "slo", Definition: "outra coisa",
	}); !errors.Is(err, ErrGlossaryTermTaken) {
		t.Fatalf("err = %v, want ErrGlossaryTermTaken", err)
	}

	// Another organization may define the same acronym.
	if _, err := svc.CreateGlossaryTerm(ctx, "org-2", "u", models.CreateGlossaryTermRequest{
		Term: "SLO", Definition: "algo diferente",
	}); err != nil {
		t.Fatalf("another organization should be free to define SLO: %v", err)
	}

	definition := "Objetivo de nível de serviço"
	updated, err := svc.UpdateGlossaryTerm(ctx, onbOrg, term.ID, models.UpdateGlossaryTermRequest{Definition: &definition})
	if err != nil {
		t.Fatalf("UpdateGlossaryTerm: %v", err)
	}
	if updated.Definition != definition {
		t.Errorf("definition = %q, want it updated", updated.Definition)
	}

	// Updating a term to keep its own name is not a clash with itself.
	sameName := "SLO"
	if _, err := svc.UpdateGlossaryTerm(ctx, onbOrg, term.ID, models.UpdateGlossaryTermRequest{Term: &sameName}); err != nil {
		t.Fatalf("renaming a term to itself should be fine: %v", err)
	}

	if err := svc.DeleteGlossaryTerm(ctx, onbOrg, term.ID); err != nil {
		t.Fatalf("DeleteGlossaryTerm: %v", err)
	}
	terms, _ := svc.ListGlossaryTerms(ctx, onbOrg)
	if len(terms) != 0 {
		t.Errorf("listed %d terms after deleting the only one", len(terms))
	}
}

func TestOnboardingService_GlossaryIsScopedToTheOrganization(t *testing.T) {
	store := newMockOnboardingStore()
	svc := NewOnboardingService(store)
	ctx := context.Background()

	term, _ := svc.CreateGlossaryTerm(ctx, onbOrg, "u", models.CreateGlossaryTermRequest{Term: "SLO", Definition: "x"})

	definition := "sequestrado"
	if _, err := svc.UpdateGlossaryTerm(ctx, "org-2", term.ID, models.UpdateGlossaryTermRequest{Definition: &definition}); !errors.Is(err, ErrGlossaryTermNotFound) {
		t.Errorf("UpdateGlossaryTerm: err = %v, want ErrGlossaryTermNotFound", err)
	}
	if err := svc.DeleteGlossaryTerm(ctx, "org-2", term.ID); !errors.Is(err, ErrGlossaryTermNotFound) {
		t.Errorf("DeleteGlossaryTerm: err = %v, want ErrGlossaryTermNotFound", err)
	}
}

func TestOnboardingService_GlossaryRejectsIncompleteTerms(t *testing.T) {
	svc := NewOnboardingService(newMockOnboardingStore())
	ctx := context.Background()

	for _, req := range []models.CreateGlossaryTermRequest{
		{Term: "  ", Definition: "algo"},
		{Term: "SLO", Definition: "   "},
	} {
		if _, err := svc.CreateGlossaryTerm(ctx, onbOrg, "u", req); !errors.Is(err, ErrGlossaryTermInvalid) {
			t.Errorf("req %+v: err = %v, want ErrGlossaryTermInvalid", req, err)
		}
	}
}
