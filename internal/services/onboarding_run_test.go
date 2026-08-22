package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"gorm.io/datatypes"
)

// ── extra mock surface the runner needs ──────────────────────────────────────

func (m *mockOnboardingStore) GetOnboardingAssignment(_ context.Context, id string) (*models.OnboardingAssignment, error) {
	for _, a := range m.assignments {
		if a.ID == id {
			copyAssignment := *a
			return &copyAssignment, nil
		}
	}
	return nil, nil
}

func (m *mockOnboardingStore) UpdateOnboardingAssignment(_ context.Context, a *models.OnboardingAssignment) error {
	for i := range m.assignments {
		if m.assignments[i].ID == a.ID {
			copyAssignment := *a
			m.assignments[i] = &copyAssignment
			return nil
		}
	}
	return nil
}

func (m *mockOnboardingStore) GetOnboardingStep(_ context.Context, id string) (*models.OnboardingStep, error) {
	for _, steps := range m.steps {
		for i := range steps {
			if steps[i].ID == id {
				copyStep := steps[i]
				return &copyStep, nil
			}
		}
	}
	return nil, nil
}

func (m *mockOnboardingStore) ListOnboardingStepProgress(_ context.Context, assignmentID string) ([]models.OnboardingStepProgress, error) {
	return append([]models.OnboardingStepProgress(nil), m.progress[assignmentID]...), nil
}

func (m *mockOnboardingStore) UpsertOnboardingStepProgress(_ context.Context, p *models.OnboardingStepProgress) error {
	rows := m.progress[p.AssignmentID]
	for i := range rows {
		if rows[i].StepID == p.StepID {
			rows[i] = *p
			m.progress[p.AssignmentID] = rows
			return nil
		}
	}
	m.progress[p.AssignmentID] = append(rows, *p)
	return nil
}

func (m *mockOnboardingStore) ListTeamMembers(_ context.Context, teamID string) ([]models.TeamMember, error) {
	return m.teamMembers[teamID], nil
}

func (m *mockOnboardingStore) ListTeamIDsForUser(_ context.Context, _ string, userID string) ([]string, error) {
	return m.userTeams[userID], nil
}

func (m *mockOnboardingStore) ListRepositories(_ context.Context, filter *storage.RepositoryFilter) ([]models.Repository, int64, error) {
	var out []models.Repository
	for id, repo := range m.repoRows {
		if repo.OrganizationID != filter.OrganizationID {
			continue
		}
		if len(filter.OwnerTeamIDs) > 0 {
			if repo.OwnerTeamID == nil || repo.OwnerTeamID != nil && *repo.OwnerTeamID != filter.OwnerTeamIDs[0] {
				continue
			}
		}
		copyRepo := *repo
		copyRepo.ID = id
		out = append(out, copyRepo)
	}
	return out, int64(len(out)), nil
}

func (m *mockOnboardingStore) ListRepositoryRelationships(_ context.Context, _ storage.RepositoryRelationshipFilter) ([]models.RepositoryRelationship, error) {
	return nil, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func newRunFixture(t *testing.T) (*mockOnboardingStore, *OnboardingService, *OnboardingRunService) {
	t.Helper()
	store := newMockOnboardingStore()
	return store, NewOnboardingService(store), NewOnboardingRunService(store, nil)
}

func assignOne(t *testing.T, store *mockOnboardingStore, svc *OnboardingService, flowID, userID string) *models.OnboardingAssignment {
	t.Helper()
	store.members[onbOrg+"/"+userID] = "member"
	assignment, err := svc.AssignToMember(context.Background(), onbOrg, "admin", models.AssignOnboardingRequest{
		FlowID: flowID, UserID: userID,
	})
	if err != nil {
		t.Fatalf("AssignToMember: %v", err)
	}
	return assignment
}

// ── resolution ───────────────────────────────────────────────────────────────

func TestOnboardingRun_ResolvesLiveEntities(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()

	teamID := "team-1"
	store.teams[teamID] = onbOrg
	store.teamMembers[teamID] = []models.TeamMember{
		{TeamID: teamID, UserID: "user-lead", Role: models.TeamRoleLead, User: models.User{FullName: "Ana", Email: "ana@e.test"}},
	}
	store.repos["repo-1"] = onbOrg
	store.repoRows["repo-1"] = &models.Repository{
		ID: "repo-1", OrganizationID: onbOrg, Name: "owner/serviço", Description: "o serviço",
		OwnerTeamID: &teamID,
	}
	store.terms["term-1"] = &models.GlossaryTerm{ID: "term-1", OrganizationID: onbOrg, Term: "SLO", Definition: "objetivo"}
	store.docs["doc-1"] = onbOrg
	store.docRows["doc-1"] = &models.DocGeneration{
		ID: "doc-1", OrganizationID: onbOrg,
		Content: datatypes.NewJSONType(map[string]string{"architecture": "# Arquitetura"}),
	}
	store.members[onbOrg+"/user-contact"] = "member"

	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	if _, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Bem-vindo", Body: "# Oi"},
		stepInput(models.OnboardingStepKindRepository, "O serviço", models.OnboardingStepConfig{RepositoryID: "repo-1"}),
		stepInput(models.OnboardingStepKindTeam, "O time", models.OnboardingStepConfig{TeamID: teamID}),
		stepInput(models.OnboardingStepKindGlossary, "Vocabulário", models.OnboardingStepConfig{}),
		stepInput(models.OnboardingStepKindDoc, "Arquitetura", models.OnboardingStepConfig{DocGenerationID: "doc-1", DocType: "architecture"}),
		stepInput(models.OnboardingStepKindContacts, "Quem procurar", models.OnboardingStepConfig{
			People: []models.OnboardingStepContact{{UserID: "user-contact", Area: "Acesso"}},
		}),
		stepInput(models.OnboardingStepKindVerified, "Primeiro PR", models.OnboardingStepConfig{
			Check: models.OnboardingCheckFirstChangeRequest, RepositoryID: "repo-1",
		}),
	}); err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	assignOne(t, store, svc, flow.ID, "user-new")

	runs, err := run.ListForUser(ctx, onbOrg, "user-new")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	steps := runs[0].Steps
	if len(steps) != 7 {
		t.Fatalf("got %d steps, want 7", len(steps))
	}

	// Editorial: nothing to resolve.
	if steps[0].Resolved != nil {
		t.Error("a markdown step should resolve nothing")
	}
	// The repository arrives whole, scorecard included, because it is read live
	// rather than copied into the step.
	if steps[1].Resolved == nil || steps[1].Resolved.Repository == nil {
		t.Fatal("the repository step resolved nothing")
	}
	if steps[1].Resolved.Repository.Name != "owner/serviço" {
		t.Errorf("repository = %+v, want the live row", steps[1].Resolved.Repository)
	}
	// The team answers both halves: who is in it, and what it answers for.
	team := steps[2].Resolved.Team
	if team == nil || len(team.Members) != 1 || team.Members[0].FullName != "Ana" {
		t.Fatalf("team = %+v, want its members", team)
	}
	if len(team.Repositories) != 1 || team.Repositories[0].ID != "repo-1" {
		t.Errorf("team repositories = %+v, want the one it owns", team.Repositories)
	}
	// An empty selection means the whole vocabulary, so terms added later show
	// up without anyone editing the flow.
	if len(steps[3].Resolved.Terms) != 1 || steps[3].Resolved.Terms[0].Term != "SLO" {
		t.Errorf("terms = %+v, want the organization's vocabulary", steps[3].Resolved.Terms)
	}
	if steps[4].Resolved.Doc == nil || steps[4].Resolved.Doc.Content != "# Arquitetura" {
		t.Errorf("doc = %+v, want the generated markdown", steps[4].Resolved.Doc)
	}
	if len(steps[5].Resolved.People) != 1 || steps[5].Resolved.People[0].Area != "Acesso" {
		t.Errorf("people = %+v, want the contact resolved", steps[5].Resolved.People)
	}
	// A verified step says what it will check, so the UI shows the claim rather
	// than a bare tick.
	if steps[6].Resolved.Verification == nil || steps[6].Resolved.Verification.Description == "" {
		t.Errorf("verification = %+v, want the claim described", steps[6].Resolved.Verification)
	}
	if steps[6].CompletionMode != models.OnboardingCompletionVerified {
		t.Errorf("completion mode = %q, want verified", steps[6].CompletionMode)
	}
}

func TestOnboardingRun_ExplainsReferencesThatWentAway(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()

	store.repos["repo-1"] = onbOrg
	store.teams["team-1"] = onbOrg
	store.docs["doc-1"] = onbOrg
	store.docRows["doc-1"] = &models.DocGeneration{ID: "doc-1", OrganizationID: onbOrg,
		Content: datatypes.NewJSONType(map[string]string{"architecture": "# A"})}

	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	if _, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		stepInput(models.OnboardingStepKindRepository, "O serviço", models.OnboardingStepConfig{RepositoryID: "repo-1"}),
		stepInput(models.OnboardingStepKindTeam, "O time", models.OnboardingStepConfig{TeamID: "team-1"}),
		stepInput(models.OnboardingStepKindDoc, "Doc", models.OnboardingStepConfig{DocGenerationID: "doc-1", DocType: "architecture"}),
	}); err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	assignOne(t, store, svc, flow.ID, "user-new")

	// Everything the flow pointed at is deleted after it was written. This is
	// ordinary: repositories get archived, teams dissolve, docs get removed.
	delete(store.repos, "repo-1")
	delete(store.repoRows, "repo-1")
	delete(store.teams, "team-1")
	delete(store.docs, "doc-1")

	runs, err := run.ListForUser(ctx, onbOrg, "user-new")
	if err != nil {
		t.Fatalf("ListForUser must not fail on a dangling reference: %v", err)
	}
	for i, step := range runs[0].Steps {
		if step.Unavailable == "" {
			t.Errorf("step %d (%s) rendered nothing and explained nothing", i, step.Kind)
		}
		if step.Resolved != nil {
			t.Errorf("step %d resolved something that no longer exists", i)
		}
	}
}

func TestOnboardingRun_ContactWhoLeftKeepsTheArea(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()

	store.members[onbOrg+"/user-gone"] = "member"
	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	if _, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		stepInput(models.OnboardingStepKindContacts, "Quem procurar", models.OnboardingStepConfig{
			People: []models.OnboardingStepContact{{UserID: "user-gone", Area: "Deploy", WhenToReach: "quando a esteira travar"}},
		}),
	}); err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	assignOne(t, store, svc, flow.ID, "user-new")

	delete(store.members, onbOrg+"/user-gone")

	runs, err := run.ListForUser(ctx, onbOrg, "user-new")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	people := runs[0].Steps[0].Resolved.People
	if len(people) != 1 {
		t.Fatalf("people = %+v, want the row kept", people)
	}
	// Knowing the area is still useful even when the person is gone; dropping
	// the row would leave the newcomer with no idea who to look for.
	if people[0].Area != "Deploy" || people[0].FullName == "" {
		t.Errorf("contact = %+v, want the area kept and the absence stated", people[0])
	}
}

// ── progress ─────────────────────────────────────────────────────────────────

func TestOnboardingRun_MarkStepAndDerivedCompletion(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()

	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	optional := false
	steps, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Obrigatório", Body: "a"},
		{Kind: models.OnboardingStepKindMarkdown, Title: "Opcional", Body: "b", IsRequired: &optional},
	})
	if err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}
	assignment := assignOne(t, store, svc, flow.ID, "user-new")

	// Before anything: one required step pending, so the walk is not done.
	runs, _ := run.ListForUser(ctx, onbOrg, "user-new")
	if runs[0].RequiredRemaining != 1 || runs[0].StepsDone != 0 {
		t.Fatalf("run = %+v, want one required step pending", runs[0])
	}

	updated, err := run.MarkStep(ctx, onbOrg, "user-new", steps[0].ID, models.MarkOnboardingStepRequest{
		Status: models.OnboardingStepDone,
	})
	if err != nil {
		t.Fatalf("MarkStep: %v", err)
	}

	// Completion is derived from "no required step pending", not asked for — so
	// finishing the required step finishes the flow even with an optional step
	// untouched.
	if updated.StepsDone != 1 || updated.RequiredRemaining != 0 {
		t.Fatalf("run = %+v, want 1 done and nothing required pending", updated)
	}
	if updated.Status != models.OnboardingAssignmentCompleted || updated.CompletedAt == nil {
		t.Fatalf("status = %q, want completed", updated.Status)
	}
	stored, _ := store.GetOnboardingAssignment(ctx, assignment.ID)
	if stored.StartedAt == nil {
		t.Error("StartedAt was never set")
	}
}

func TestOnboardingRun_SkippingARequiredStepStillCompletesIt(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()

	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	steps, _ := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Um", Body: "a"},
	})
	assignOne(t, store, svc, flow.ID, "user-new")

	updated, err := run.MarkStep(ctx, onbOrg, "user-new", steps[0].ID, models.MarkOnboardingStepRequest{
		Status: models.OnboardingStepSkipped, Note: "já sei isso",
	})
	if err != nil {
		t.Fatalf("MarkStep: %v", err)
	}

	// A skip is an outcome, not a hole: the person said something about the
	// step, and the note records what. It counts as answered but not as done.
	if updated.RequiredRemaining != 0 {
		t.Errorf("RequiredRemaining = %d, want 0 — the step was answered", updated.RequiredRemaining)
	}
	if updated.StepsDone != 0 {
		t.Errorf("StepsDone = %d, want 0 — skipped is not done", updated.StepsDone)
	}
	if updated.Steps[0].Note != "já sei isso" {
		t.Errorf("note = %q, want it kept", updated.Steps[0].Note)
	}
}

func TestOnboardingRun_AddingARequiredStepReopensTheFlow(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()

	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	steps, _ := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Um", Body: "a"},
	})
	assignOne(t, store, svc, flow.ID, "user-new")

	if _, err := run.MarkStep(ctx, onbOrg, "user-new", steps[0].ID, models.MarkOnboardingStepRequest{
		Status: models.OnboardingStepDone,
	}); err != nil {
		t.Fatalf("MarkStep: %v", err)
	}

	// The admin adds a step to a flow somebody already finished, passing the
	// existing step's id back as the builder does.
	if _, err := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{ID: steps[0].ID, Kind: models.OnboardingStepKindMarkdown, Title: "Um", Body: "a"},
		{Kind: models.OnboardingStepKindMarkdown, Title: "Dois", Body: "b"},
	}); err != nil {
		t.Fatalf("ReplaceSteps: %v", err)
	}

	runs, err := run.ListForUser(ctx, onbOrg, "user-new")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	// Live edits mean exactly this: the new step is pending for someone who had
	// finished, and their earlier progress is untouched.
	if runs[0].RequiredRemaining != 1 {
		t.Errorf("RequiredRemaining = %d, want the new step pending", runs[0].RequiredRemaining)
	}
	if runs[0].StepsDone != 1 {
		t.Errorf("StepsDone = %d, want the earlier progress kept", runs[0].StepsDone)
	}
}

func TestOnboardingRun_MarkStepRejectsForeignSteps(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()

	mine, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Meu"})
	theirs, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Deles"})
	_, _ = svc.ReplaceSteps(ctx, onbOrg, mine.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Meu", Body: "a"},
	})
	otherSteps, _ := svc.ReplaceSteps(ctx, onbOrg, theirs.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Deles", Body: "b"},
	})
	assignOne(t, store, svc, mine.ID, "user-new")

	// A step from a flow this person was never assigned: marking it would be
	// writing progress for an onboarding that is not theirs.
	if _, err := run.MarkStep(ctx, onbOrg, "user-new", otherSteps[0].ID, models.MarkOnboardingStepRequest{
		Status: models.OnboardingStepDone,
	}); !errors.Is(err, ErrOnboardingAssignmentNotFound) {
		t.Fatalf("err = %v, want ErrOnboardingAssignmentNotFound", err)
	}
}

func TestOnboardingRun_MarkStepRejectsUnknownStatus(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	steps, _ := svc.ReplaceSteps(ctx, onbOrg, flow.ID, []models.OnboardingStepInput{
		{Kind: models.OnboardingStepKindMarkdown, Title: "Um", Body: "a"},
	})
	assignOne(t, store, svc, flow.ID, "user-new")

	if _, err := run.MarkStep(ctx, onbOrg, "user-new", steps[0].ID, models.MarkOnboardingStepRequest{
		Status: "pending",
	}); !errors.Is(err, ErrOnboardingStepStatusInvalid) {
		t.Fatalf("err = %v, want ErrOnboardingStepStatusInvalid", err)
	}
}

func TestOnboardingRun_Feedback(t *testing.T) {
	store, svc, run := newRunFixture(t)
	ctx := context.Background()
	flow, _ := svc.CreateFlow(ctx, onbOrg, "admin", models.CreateOnboardingFlowRequest{Name: "Geral"})
	assignment := assignOne(t, store, svc, flow.ID, "user-new")

	if err := run.SubmitFeedback(ctx, onbOrg, "user-new", assignment.ID, models.OnboardingFeedbackRequest{
		Feedback: "faltou explicar como pedir acesso ao staging",
	}); err != nil {
		t.Fatalf("SubmitFeedback: %v", err)
	}

	stored, _ := store.GetOnboardingAssignment(ctx, assignment.ID)
	if stored.Feedback == "" || stored.FeedbackAt == nil {
		t.Fatalf("assignment = %+v, want the feedback stored with a timestamp", stored)
	}

	// Somebody else's assignment is not theirs to comment on.
	if err := run.SubmitFeedback(ctx, onbOrg, "intruder", assignment.ID, models.OnboardingFeedbackRequest{
		Feedback: "não é meu",
	}); !errors.Is(err, ErrOnboardingAssignmentNotFound) {
		t.Errorf("err = %v, want ErrOnboardingAssignmentNotFound", err)
	}
}

// ── verification ─────────────────────────────────────────────────────────────

func TestOnboardingVerifier_TeamMembership(t *testing.T) {
	store := newMockOnboardingStore()
	verifier := NewOnboardingVerifier(store, scmCredentialsForTest())
	ctx := context.Background()

	store.teams["team-1"] = onbOrg
	step := &models.OnboardingStep{Kind: models.OnboardingStepKindVerified, Title: "Time"}
	if err := step.SetConfig(models.OnboardingStepConfig{
		Check: models.OnboardingCheckTeamMembership, TeamID: "team-1",
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	result, err := verifier.Verify(ctx, onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Passed {
		t.Error("passed for someone in no team")
	}
	// Even a negative has to say what was checked.
	if result.How == "" {
		t.Error("the result explains nothing")
	}

	store.userTeams["user-new"] = []string{"team-1"}
	result, err = verifier.Verify(ctx, onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Passed || result.Pending {
		t.Fatalf("result = %+v, want a pass", result)
	}
}

func TestOnboardingVerifier_TeamMembershipAnyTeam(t *testing.T) {
	store := newMockOnboardingStore()
	verifier := NewOnboardingVerifier(store, scmCredentialsForTest())

	step := &models.OnboardingStep{Kind: models.OnboardingStepKindVerified, Title: "Algum time"}
	_ = step.SetConfig(models.OnboardingStepConfig{Check: models.OnboardingCheckTeamMembership})

	store.userTeams["user-new"] = []string{"whatever"}
	result, err := verifier.Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Passed {
		t.Fatalf("result = %+v, want any team to satisfy an unspecified team", result)
	}
}

func TestOnboardingVerifier_FirstChangeRequestPendingWithoutAProviderLogin(t *testing.T) {
	store := newMockOnboardingStore()
	verifier := NewOnboardingVerifier(store, scmCredentialsForTest())

	store.repos["repo-1"] = onbOrg
	store.repoRows["repo-1"] = &models.Repository{
		ID: "repo-1", OrganizationID: onbOrg, Name: "owner/repo",
		URL: "https://github.com/owner/repo",
	}

	step := &models.OnboardingStep{Kind: models.OnboardingStepKindVerified, Title: "Primeiro PR"}
	_ = step.SetConfig(models.OnboardingStepConfig{
		Check: models.OnboardingCheckFirstChangeRequest, RepositoryID: "repo-1",
	})

	result, err := verifier.Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Nobody connected the provider, so we cannot look. Reporting a failure
	// would tell someone they never opened a pull request when we never looked.
	if !result.Pending || result.Passed {
		t.Fatalf("result = %+v, want pending", result)
	}
}

func TestOnboardingVerifier_FirstChangeRequestPendingWhenTheRepositoryIsGone(t *testing.T) {
	store := newMockOnboardingStore()
	verifier := NewOnboardingVerifier(store, scmCredentialsForTest())

	step := &models.OnboardingStep{Kind: models.OnboardingStepKindVerified, Title: "Primeiro PR"}
	_ = step.SetConfig(models.OnboardingStepConfig{
		Check: models.OnboardingCheckFirstChangeRequest, RepositoryID: "repo-gone",
	})

	result, err := verifier.Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Pending {
		t.Fatalf("result = %+v, want pending rather than a failure", result)
	}
}

func TestOnboardingVerifier_UnknownCheckIsPending(t *testing.T) {
	store := newMockOnboardingStore()
	verifier := NewOnboardingVerifier(store, scmCredentialsForTest())

	step := &models.OnboardingStep{Kind: models.OnboardingStepKindVerified, Title: "?"}
	_ = step.SetConfig(models.OnboardingStepConfig{Check: "reads_minds"})

	result, err := verifier.Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// A step stored by a newer version than this build must not read as failed.
	if !result.Pending || result.Passed {
		t.Fatalf("result = %+v, want pending", result)
	}
}

// scmCredentialsForTest builds host-only credentials, which is what the
// verifier is given in production too.
func scmCredentialsForTest() scm.Credentials {
	return scm.HostsOnly("")
}

// The verifier resolves the person's provider identity. Returning nil here is
// the case the tests exercise: nobody has signed in through the provider yet.
func (m *mockOnboardingStore) GetOAuthConnectionByUser(_ context.Context, userID, provider string) (*models.OAuthConnection, error) {
	return m.connections[userID+"/"+provider], nil
}

func (m *mockOnboardingStore) GetOrganizationConfig(_ context.Context, _ string) (*models.OrganizationConfig, error) {
	return m.orgConfig, nil
}

// changeRequestProvider is an scm.Provider that only answers the one call the
// verification makes. Embedding the interface leaves everything else nil, which
// is exactly right: a verification reaching for anything more would be doing
// something this test did not authorize.
type changeRequestProvider struct {
	scm.Provider
	changeRequests []scm.ChangeRequest
	err            error
}

func (p *changeRequestProvider) ListChangeRequests(context.Context, scm.RepoRef) ([]scm.ChangeRequest, error) {
	return p.changeRequests, p.err
}

func verifierWithProvider(store *mockOnboardingStore, provider scm.Provider, err error) *OnboardingVerifier {
	verifier := NewOnboardingVerifier(store, scm.HostsOnly(""))
	verifier.resolve = func(models.RepositoryType, scm.Credentials) (scm.Provider, error) {
		if err != nil {
			return nil, err
		}
		return provider, nil
	}
	return verifier
}

func firstChangeRequestFixture(t *testing.T) (*mockOnboardingStore, *models.OnboardingStep) {
	t.Helper()
	store := newMockOnboardingStore()
	store.repos["repo-1"] = onbOrg
	store.repoRows["repo-1"] = &models.Repository{
		ID: "repo-1", OrganizationID: onbOrg, Name: "owner/repo",
		URL: "https://github.com/owner/repo",
	}
	store.connections["user-new/github"] = &models.OAuthConnection{ProviderUsername: "octocat"}
	store.orgConfig = &models.OrganizationConfig{OrganizationID: onbOrg, GithubToken: "gh"}

	step := &models.OnboardingStep{Kind: models.OnboardingStepKindVerified, Title: "Primeiro PR"}
	if err := step.SetConfig(models.OnboardingStepConfig{
		Check: models.OnboardingCheckFirstChangeRequest, RepositoryID: "repo-1",
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	return store, step
}

func TestOnboardingVerifier_FirstChangeRequestFound(t *testing.T) {
	store, step := firstChangeRequestFixture(t)
	provider := &changeRequestProvider{changeRequests: []scm.ChangeRequest{
		{Number: 3, AuthorLogin: "someone-else"},
		// The provider's casing need not match what we stored.
		{Number: 7, AuthorLogin: "OctoCat"},
	}}

	result, err := verifierWithProvider(store, provider, nil).Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Passed || result.Pending {
		t.Fatalf("result = %+v, want a pass", result)
	}
	// The evidence is the point: a verification that cannot say what it found is
	// no better than a checkbox.
	if !strings.Contains(result.How, "#7") || !strings.Contains(result.How, "octocat") && !strings.Contains(result.How, "OctoCat") {
		t.Errorf("How = %q, want it to name the change request it found", result.How)
	}
}

func TestOnboardingVerifier_FirstChangeRequestNotFoundIsAHonestNegative(t *testing.T) {
	store, step := firstChangeRequestFixture(t)
	provider := &changeRequestProvider{changeRequests: []scm.ChangeRequest{
		{Number: 3, AuthorLogin: "someone-else"},
	}}

	result, err := verifierWithProvider(store, provider, nil).Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// We did look, and nothing was there: a real negative, not pending.
	if result.Passed || result.Pending {
		t.Fatalf("result = %+v, want a plain negative", result)
	}
	// Only open change requests are listed, so the message has to say what was
	// searched rather than claim nothing was ever opened.
	if !strings.Contains(result.How, "aberto") {
		t.Errorf("How = %q, want it to state which window was searched", result.How)
	}
}

func TestOnboardingVerifier_ProviderFailureIsPending(t *testing.T) {
	store, step := firstChangeRequestFixture(t)
	provider := &changeRequestProvider{err: errors.New("gateway timeout")}

	result, err := verifierWithProvider(store, provider, nil).Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify should not surface a provider failure as an error: %v", err)
	}
	// The provider was unreachable. Telling someone they have not opened a pull
	// request when we could not look is the failure worth avoiding here.
	if result.Passed || !result.Pending {
		t.Fatalf("result = %+v, want pending", result)
	}
}

func TestOnboardingVerifier_MissingProviderTokenIsPending(t *testing.T) {
	store, step := firstChangeRequestFixture(t)
	store.orgConfig = &models.OrganizationConfig{OrganizationID: onbOrg}

	result, err := verifierWithProvider(store, nil, scm.ErrMissingCredentials).Verify(context.Background(), onbOrg, "user-new", step)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Passed || !result.Pending {
		t.Fatalf("result = %+v, want pending — the organization configured no token", result)
	}
}
