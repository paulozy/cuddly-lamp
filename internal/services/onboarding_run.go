package services

import (
	"context"
	"errors"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

var (
	ErrOnboardingAssignmentNotFound = errors.New("onboarding assignment not found")
	ErrOnboardingStepNotFound       = errors.New("onboarding step not found")
	ErrOnboardingStepStatusInvalid  = errors.New("step status must be done or skipped")
)

// OnboardingRunService serves the person walking a flow: it resolves each step
// against live data, records progress, and collects the closing feedback.
//
// Resolution happens here, on the server, for three reasons that all point the
// same way: one request instead of one per step, one place where authorization
// is applied, and one place that handles a reference whose entity is gone.
type OnboardingRunService struct {
	repo     storage.Repository
	graph    *RepositoryRelationshipService
	verifier *OnboardingVerifier
}

func NewOnboardingRunService(repo storage.Repository, verifier *OnboardingVerifier) *OnboardingRunService {
	return &OnboardingRunService{
		repo:     repo,
		graph:    NewRepositoryRelationshipService(repo),
		verifier: verifier,
	}
}

// ListForUser returns every live assignment the caller has, resolved and ready
// to render.
func (s *OnboardingRunService) ListForUser(ctx context.Context, orgID, userID string) ([]models.OnboardingRunResponse, error) {
	assignments, err := s.repo.ListOnboardingAssignmentsForUser(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}

	out := make([]models.OnboardingRunResponse, 0, len(assignments))
	for i := range assignments {
		run, err := s.buildRun(ctx, orgID, &assignments[i])
		if err != nil {
			return nil, err
		}
		if run != nil {
			out = append(out, *run)
		}
	}
	return out, nil
}

// buildRun assembles one assignment. A flow deleted under a live assignment
// yields nil: there is nothing to walk, and the person should not be shown an
// empty shell.
func (s *OnboardingRunService) buildRun(ctx context.Context, orgID string, assignment *models.OnboardingAssignment) (*models.OnboardingRunResponse, error) {
	flow, err := s.repo.GetOnboardingFlow(ctx, assignment.FlowID)
	if err != nil {
		return nil, err
	}
	if flow == nil || flow.OrganizationID != orgID {
		return nil, nil
	}

	steps, err := s.repo.ListOnboardingSteps(ctx, flow.ID)
	if err != nil {
		return nil, err
	}
	progress, err := s.repo.ListOnboardingStepProgress(ctx, assignment.ID)
	if err != nil {
		return nil, err
	}
	byStep := make(map[string]models.OnboardingStepProgress, len(progress))
	for i := range progress {
		byStep[progress[i].StepID] = progress[i]
	}

	run := &models.OnboardingRunResponse{
		AssignmentID: assignment.ID,
		Status:       assignment.Status,
		FlowID:       flow.ID,
		FlowName:     flow.Name,
		FlowSummary:  flow.Description,
		StepsTotal:   len(steps),
		StartedAt:    assignment.StartedAt,
		CompletedAt:  assignment.CompletedAt,
		Feedback:     assignment.Feedback,
		Steps:        make([]models.OnboardingRunStep, 0, len(steps)),
	}

	for i := range steps {
		step := steps[i]
		runStep := models.OnboardingRunStep{OnboardingStepResponse: models.OnboardingStepToResponse(&step)}

		if recorded, ok := byStep[step.ID]; ok {
			runStep.Status = recorded.Status
			runStep.Note = recorded.Note
			completed := recorded.CompletedAt
			runStep.CompletedAt = &completed
			if recorded.Status == models.OnboardingStepDone {
				run.StepsDone++
			}
		} else if step.IsRequired {
			run.RequiredRemaining++
		}

		resolved, unavailable, err := s.resolveStep(ctx, orgID, &step)
		if err != nil {
			return nil, err
		}
		runStep.Resolved = resolved
		runStep.Unavailable = unavailable

		if step.EstimatedMinutes != nil {
			run.TotalMinutes += *step.EstimatedMinutes
		}

		run.Steps = append(run.Steps, runStep)
	}

	return run, nil
}

// resolveStep loads whatever the step points at.
//
// A missing entity is never an error: it returns an explanation to render in
// its place. Repositories get deleted, documents get removed, people leave, and
// none of that should stop somebody's onboarding at step four with a 500.
func (s *OnboardingRunService) resolveStep(ctx context.Context, orgID string, step *models.OnboardingStep) (*models.OnboardingStepResolved, string, error) {
	cfg, err := step.DecodeConfig()
	if err != nil {
		// A corrupt config degrades one step, not the flow.
		return nil, "Este passo está com a configuração corrompida.", nil
	}

	switch step.Kind {
	case models.OnboardingStepKindRepository:
		repo, err := s.repo.GetRepository(ctx, cfg.RepositoryID)
		if err != nil {
			return nil, "", err
		}
		if repo == nil || repo.OrganizationID != orgID {
			return nil, "Este repositório não existe mais.", nil
		}
		return &models.OnboardingStepResolved{Repository: models.RepositoryToResponse(repo)}, "", nil

	case models.OnboardingStepKindTeam:
		return s.resolveTeam(ctx, orgID, cfg.TeamID)

	case models.OnboardingStepKindDoc:
		return s.resolveDoc(ctx, orgID, cfg)

	case models.OnboardingStepKindArchitecture:
		graph, err := s.graph.GetGraph(ctx, orgID, RepositoryGraphFilter{})
		if err != nil {
			return nil, "", err
		}
		if len(cfg.RepositoryIDs) > 0 {
			graph = filterGraph(graph, cfg.RepositoryIDs)
		}
		return &models.OnboardingStepResolved{Graph: graph}, "", nil

	case models.OnboardingStepKindGlossary:
		return s.resolveGlossary(ctx, orgID, cfg.TermIDs)

	case models.OnboardingStepKindContacts:
		return s.resolveContacts(ctx, orgID, cfg.People)

	case models.OnboardingStepKindVerified:
		return &models.OnboardingStepResolved{
			Verification: &models.OnboardingVerificationState{
				Check:       cfg.Check,
				Description: verificationDescription(cfg.Check),
			},
		}, "", nil

	default:
		// Editorial kinds carry everything they need on the step itself.
		return nil, "", nil
	}
}

func (s *OnboardingRunService) resolveTeam(ctx context.Context, orgID, teamID string) (*models.OnboardingStepResolved, string, error) {
	team, err := s.repo.GetTeam(ctx, teamID)
	if err != nil {
		return nil, "", err
	}
	if team == nil || team.OrganizationID != orgID {
		return nil, "Este time não existe mais.", nil
	}

	members, err := s.repo.ListTeamMembers(ctx, team.ID)
	if err != nil {
		return nil, "", err
	}
	memberResponses := make([]models.TeamMemberResponse, 0, len(members))
	for i := range members {
		memberResponses = append(memberResponses, models.TeamMemberResponse{
			UserID:   members[i].UserID,
			Email:    members[i].User.Email,
			FullName: members[i].User.FullName,
			Role:     members[i].Role,
		})
	}

	// "What does this team answer for" is the other half of the question, and
	// the filter that answers it is the one exposed on the repository list.
	owned, _, err := s.repo.ListRepositories(ctx, &storage.RepositoryFilter{
		OrganizationID: orgID,
		OwnerTeamIDs:   []string{team.ID},
		Limit:          100,
	})
	if err != nil {
		return nil, "", err
	}
	repos := make([]models.TeamRepositoryRef, 0, len(owned))
	for i := range owned {
		repos = append(repos, models.TeamRepositoryRef{
			ID:          owned[i].ID,
			Name:        owned[i].Name,
			Description: owned[i].Description,
		})
	}

	return &models.OnboardingStepResolved{Team: &models.OnboardingResolvedTeam{
		ID:           team.ID,
		Name:         team.Name,
		Slug:         team.Slug,
		Description:  team.Description,
		Members:      memberResponses,
		Repositories: repos,
	}}, "", nil
}

func (s *OnboardingRunService) resolveDoc(ctx context.Context, orgID string, cfg models.OnboardingStepConfig) (*models.OnboardingStepResolved, string, error) {
	doc, err := s.repo.GetDocGeneration(ctx, cfg.DocGenerationID)
	if err != nil {
		return nil, "", err
	}
	if doc == nil || doc.OrganizationID != orgID {
		return nil, "Esta documentação não existe mais.", nil
	}

	content := doc.Content.Data()
	body, ok := content[cfg.DocType]
	if !ok {
		// The step may not name a type, or the generation may have produced a
		// single document under another key: fall back to the only one there is.
		if cfg.DocType == "" && len(content) == 1 {
			for _, only := range content {
				body = only
			}
		}
	}
	if body == "" {
		return nil, "Esta documentação ainda não tem conteúdo gerado.", nil
	}

	return &models.OnboardingStepResolved{Doc: &models.OnboardingResolvedDoc{
		ID:        doc.ID,
		DocType:   cfg.DocType,
		Content:   body,
		CreatedAt: doc.CreatedAt,
	}}, "", nil
}

func (s *OnboardingRunService) resolveGlossary(ctx context.Context, orgID string, termIDs []string) (*models.OnboardingStepResolved, string, error) {
	terms, err := s.repo.ListGlossaryTerms(ctx, orgID)
	if err != nil {
		return nil, "", err
	}

	selected := make([]models.GlossaryTermResponse, 0, len(terms))
	if len(termIDs) == 0 {
		// No selection means the whole vocabulary, which stays current as terms
		// are added without anyone editing the flow.
		for i := range terms {
			selected = append(selected, models.GlossaryTermToResponse(&terms[i]))
		}
	} else {
		wanted := make(map[string]bool, len(termIDs))
		for _, id := range termIDs {
			wanted[id] = true
		}
		for i := range terms {
			if wanted[terms[i].ID] {
				selected = append(selected, models.GlossaryTermToResponse(&terms[i]))
			}
		}
	}

	if len(selected) == 0 {
		return nil, "Nenhum termo cadastrado ainda.", nil
	}
	return &models.OnboardingStepResolved{Terms: selected}, "", nil
}

func (s *OnboardingRunService) resolveContacts(ctx context.Context, orgID string, people []models.OnboardingStepContact) (*models.OnboardingStepResolved, string, error) {
	members, err := s.repo.ListOrganizationMembers(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	byUser := make(map[string]models.OrganizationMember, len(members))
	for i := range members {
		byUser[members[i].UserID] = members[i]
	}

	contacts := make([]models.OnboardingResolvedContact, 0, len(people))
	for i := range people {
		contact := models.OnboardingResolvedContact{
			UserID:      people[i].UserID,
			Area:        people[i].Area,
			WhenToReach: people[i].WhenToReach,
		}
		if member, ok := byUser[people[i].UserID]; ok {
			contact.FullName = member.User.FullName
			contact.Email = member.User.Email
		} else {
			// Somebody left. Naming the area without the person is more useful
			// than dropping the row, since it still says who to look for.
			contact.FullName = "(saiu da organização)"
		}
		contacts = append(contacts, contact)
	}

	if len(contacts) == 0 {
		return nil, "Nenhum contato configurado.", nil
	}
	return &models.OnboardingStepResolved{People: contacts}, "", nil
}

// filterGraph narrows a graph to the chosen repositories, keeping only edges
// whose both ends survived — an edge to a repository that is not shown would
// draw a line into nowhere.
func filterGraph(graph *models.RepositoryGraphResponse, repositoryIDs []string) *models.RepositoryGraphResponse {
	if graph == nil {
		return nil
	}
	wanted := make(map[string]bool, len(repositoryIDs))
	for _, id := range repositoryIDs {
		wanted[id] = true
	}

	filtered := &models.RepositoryGraphResponse{}
	for i := range graph.Nodes {
		if wanted[graph.Nodes[i].ID] {
			filtered.Nodes = append(filtered.Nodes, graph.Nodes[i])
		}
	}
	for i := range graph.Edges {
		if wanted[graph.Edges[i].SourceRepositoryID] && wanted[graph.Edges[i].TargetRepositoryID] {
			filtered.Edges = append(filtered.Edges, graph.Edges[i])
		}
	}
	return filtered
}

func verificationDescription(check models.OnboardingVerifiedCheck) string {
	switch check {
	case models.OnboardingCheckFirstChangeRequest:
		return "A plataforma confirma quando você abrir seu primeiro pull ou merge request neste repositório."
	case models.OnboardingCheckTeamMembership:
		return "A plataforma confirma quando você estiver num time."
	default:
		return ""
	}
}

// ── progress ─────────────────────────────────────────────────────────────────

// MarkStep records an outcome for one step of the caller's own assignment.
func (s *OnboardingRunService) MarkStep(ctx context.Context, orgID, userID, stepID string, req models.MarkOnboardingStepRequest) (*models.OnboardingRunResponse, error) {
	if !req.Status.IsKnown() {
		return nil, ErrOnboardingStepStatusInvalid
	}

	assignment, step, err := s.resolveOwnStep(ctx, orgID, userID, stepID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := s.repo.UpsertOnboardingStepProgress(ctx, &models.OnboardingStepProgress{
		AssignmentID: assignment.ID,
		StepID:       step.ID,
		Status:       req.Status,
		Note:         req.Note,
		CompletedAt:  now,
	}); err != nil {
		return nil, err
	}

	assignment.MarkStarted(now)
	if err := s.repo.UpdateOnboardingAssignment(ctx, assignment); err != nil {
		return nil, err
	}

	run, err := s.buildRun(ctx, orgID, assignment)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrOnboardingAssignmentNotFound
	}

	// Completion is derived, never asked for: the moment no required step is
	// pending, the walk is done. Deriving it means an admin adding a required
	// step reopens the flow, which is the honest consequence of live edits.
	if run.RequiredRemaining == 0 && assignment.Status != models.OnboardingAssignmentCompleted {
		assignment.MarkCompleted(now)
		if err := s.repo.UpdateOnboardingAssignment(ctx, assignment); err != nil {
			return nil, err
		}
		run.Status = assignment.Status
		run.CompletedAt = assignment.CompletedAt
	}

	return run, nil
}

// Verify runs a verified step's check on demand.
//
// On demand and not on load: the check talks to the provider, and doing it
// every time the runner renders would turn navigation into latency and burn
// through rate limits for no new information.
func (s *OnboardingRunService) Verify(ctx context.Context, orgID, userID, stepID string) (*models.OnboardingVerificationResult, error) {
	assignment, step, err := s.resolveOwnStep(ctx, orgID, userID, stepID)
	if err != nil {
		return nil, err
	}
	if step.Kind != models.OnboardingStepKindVerified {
		return nil, ErrOnboardingStepNotFound
	}
	if s.verifier == nil {
		return &models.OnboardingVerificationResult{
			Pending: true,
			How:     "A verificação automática não está disponível nesta instalação.",
		}, nil
	}

	result, err := s.verifier.Verify(ctx, orgID, userID, step)
	if err != nil {
		return nil, err
	}

	// Only a pass is recorded. A failed check is a moment in time, not an
	// outcome — the person may open that pull request an hour later.
	if result.Passed {
		now := time.Now().UTC()
		if err := s.repo.UpsertOnboardingStepProgress(ctx, &models.OnboardingStepProgress{
			AssignmentID: assignment.ID,
			StepID:       step.ID,
			Status:       models.OnboardingStepDone,
			Note:         result.How,
			CompletedAt:  now,
		}); err != nil {
			return nil, err
		}
		assignment.MarkStarted(now)
		if err := s.repo.UpdateOnboardingAssignment(ctx, assignment); err != nil {
			return nil, err
		}
	}

	return &result, nil
}

// SubmitFeedback stores what the newcomer says was missing.
//
// They are the only person who knows, and in two weeks they will have forgotten
// they ever didn't know — which is why this is asked at the end rather than
// left to a retrospective.
func (s *OnboardingRunService) SubmitFeedback(ctx context.Context, orgID, userID, assignmentID string, req models.OnboardingFeedbackRequest) error {
	assignment, err := s.repo.GetOnboardingAssignment(ctx, assignmentID)
	if err != nil {
		return err
	}
	if assignment == nil || assignment.OrganizationID != orgID || assignment.UserID != userID {
		return ErrOnboardingAssignmentNotFound
	}

	now := time.Now().UTC()
	assignment.Feedback = req.Feedback
	assignment.FeedbackAt = &now
	return s.repo.UpdateOnboardingAssignment(ctx, assignment)
}

// resolveOwnStep loads a step together with the caller's assignment for its
// flow, which is what makes "mark this step" unable to touch anyone else's
// progress — or a step from a flow the caller was never assigned.
func (s *OnboardingRunService) resolveOwnStep(ctx context.Context, orgID, userID, stepID string) (*models.OnboardingAssignment, *models.OnboardingStep, error) {
	step, err := s.repo.GetOnboardingStep(ctx, stepID)
	if err != nil {
		return nil, nil, err
	}
	if step == nil {
		return nil, nil, ErrOnboardingStepNotFound
	}

	assignments, err := s.repo.ListOnboardingAssignmentsForUser(ctx, orgID, userID)
	if err != nil {
		return nil, nil, err
	}
	for i := range assignments {
		if assignments[i].FlowID == step.FlowID {
			return &assignments[i], step, nil
		}
	}
	return nil, nil, ErrOnboardingAssignmentNotFound
}
