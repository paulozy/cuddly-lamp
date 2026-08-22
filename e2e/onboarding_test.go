//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/testsupport/fakegitlab"
)

// The onboarding path end to end: an admin composes a flow, invites someone
// with it, and that person walks it. Every step kind that can be exercised
// without a browser is included, and so are the ways it goes wrong — a deleted
// repository mid-flow, a step added under someone who had finished.

type flowResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	IsDefault   bool   `json:"is_default"`
	StepCount   int    `json:"step_count"`
	TotalMinues int    `json:"total_minutes"`
	Steps       []struct {
		ID             string `json:"id"`
		Kind           string `json:"kind"`
		Title          string `json:"title"`
		IsRequired     bool   `json:"is_required"`
		CompletionMode string `json:"completion_mode"`
	} `json:"steps"`
}

type runResponse struct {
	AssignmentID      string `json:"assignment_id"`
	Status            string `json:"status"`
	FlowName          string `json:"flow_name"`
	StepsTotal        int    `json:"steps_total"`
	StepsDone         int    `json:"steps_done"`
	RequiredRemaining int    `json:"required_remaining"`
	Feedback          string `json:"feedback"`
	Steps             []struct {
		ID             string `json:"id"`
		Kind           string `json:"kind"`
		Title          string `json:"title"`
		Status         string `json:"status"`
		Note           string `json:"note"`
		CompletionMode string `json:"completion_mode"`
		Unavailable    string `json:"unavailable"`
		Resolved       *struct {
			Repository *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"repository"`
			Team *struct {
				Name    string `json:"name"`
				Members []struct {
					FullName string `json:"full_name"`
				} `json:"members"`
				Repositories []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"repositories"`
			} `json:"team"`
			Terms []struct {
				Term string `json:"term"`
			} `json:"terms"`
			People []struct {
				FullName string `json:"full_name"`
				Area     string `json:"area"`
			} `json:"people"`
			Verification *struct {
				Check       string `json:"check"`
				Description string `json:"description"`
			} `json:"verification"`
		} `json:"resolved"`
	} `json:"steps"`
}

type verificationResponse struct {
	Passed  bool   `json:"passed"`
	Pending bool   `json:"pending"`
	How     string `json:"how"`
}

func (s *stack) myOnboarding(t *testing.T, token string) []runResponse {
	t.Helper()
	resp := s.mustDo(t, http.MethodGet, "/api/v1/onboarding/me", token, nil, http.StatusOK)
	var list struct {
		Items []runResponse `json:"items"`
	}
	resp.decode(t, &list)
	return list.Items
}

func TestOnboardingLifecycle(t *testing.T) {
	adminToken := sut.registerOrg(t, "onboarding")
	configureGitLabToken(t, adminToken)

	// A repository and a team, so the referential steps have something real to
	// point at — which is the whole premise of the feature.
	repo := sut.createRepository(t, adminToken, runnerURL)
	synced := sut.waitForSync(t, adminToken, repo.ID)
	if synced.SyncStatus != "synced" {
		t.Fatalf("sync_status = %q, want synced", synced.SyncStatus)
	}

	teamResp := sut.mustDo(t, http.MethodPost, "/api/v1/teams", adminToken, map[string]any{
		"name": "Plataforma",
	}, http.StatusCreated)
	var team struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	teamResp.decode(t, &team)

	sut.mustDo(t, http.MethodPut, "/api/v1/repositories/"+repo.ID+"/owner", adminToken, map[string]any{
		"team_id": team.ID,
	}, http.StatusNoContent)

	sut.mustDo(t, http.MethodPost, "/api/v1/glossary", adminToken, map[string]any{
		"term": "SLO", "definition": "Objetivo de nível de serviço",
	}, http.StatusCreated)

	// The admin is a member, so they can be a contact.
	membersResp := sut.mustDo(t, http.MethodGet, "/api/v1/organizations/members", adminToken, nil, http.StatusOK)
	var members struct {
		Items []struct {
			UserID   string `json:"user_id"`
			FullName string `json:"full_name"`
		} `json:"items"`
	}
	membersResp.decode(t, &members)
	if len(members.Items) == 0 {
		t.Fatal("the organization reports no members")
	}
	adminUserID := members.Items[0].UserID

	var flow flowResponse

	t.Run("a flow created from a template arrives with valid steps", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodPost, "/api/v1/onboarding/flows", adminToken, map[string]any{
			"name":        "Dev Backend",
			"template_id": "backend-dev",
			"is_default":  true,
		}, http.StatusCreated)
		resp.decode(t, &flow)

		if flow.StepCount == 0 {
			t.Fatalf("flow = %+v, want the template's steps materialized", flow)
		}
		if !flow.IsDefault {
			t.Error("the flow was not marked as the organization default")
		}
	})

	t.Run("the step list is replaced with one of each kind", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodPut, "/api/v1/onboarding/flows/"+flow.ID+"/steps", adminToken, map[string]any{
			"steps": []map[string]any{
				{"kind": "markdown", "title": "Bem-vindo", "body": "# Olá", "is_required": true, "estimated_minutes": 5},
				{"kind": "repository", "title": "Nosso serviço", "config": map[string]any{"repository_id": repo.ID}},
				{"kind": "team", "title": "Quem responde", "config": map[string]any{"team_id": team.ID}},
				{"kind": "glossary", "title": "Vocabulário", "is_required": false},
				{"kind": "contacts", "title": "Quem procurar", "config": map[string]any{
					"people": []map[string]any{{"user_id": adminUserID, "area": "Acesso", "when_to_reach": "quando faltar permissão"}},
				}},
				{"kind": "checklist", "title": "Ambiente", "config": map[string]any{
					"items": []map[string]any{{"text": "Clonar o repo"}},
				}},
				{"kind": "link", "title": "Wiki", "config": map[string]any{"url": "https://wiki.example", "label": "Abrir"}},
				{"kind": "verified", "title": "Você está num time", "config": map[string]any{"check": "team_membership"}},
				{"kind": "task", "title": "Primeira tarefa", "config": map[string]any{"instructions": "Pegue a primeira issue."}},
			},
		}, http.StatusOK)
		resp.decode(t, &flow)

		if len(flow.Steps) != 9 {
			t.Fatalf("saved %d steps, want 9", len(flow.Steps))
		}
		// The completion mode is decided server-side per kind, and the UI is
		// built on it: a task is self-reported, a verified step is verified.
		modes := map[string]string{}
		for _, step := range flow.Steps {
			modes[step.Kind] = step.CompletionMode
		}
		if modes["task"] != "self_reported" {
			t.Errorf("task completion mode = %q, want self_reported", modes["task"])
		}
		if modes["verified"] != "verified" {
			t.Errorf("verified completion mode = %q, want verified", modes["verified"])
		}
		if modes["markdown"] != "acknowledge" {
			t.Errorf("required markdown mode = %q, want acknowledge", modes["markdown"])
		}
		if modes["glossary"] != "auto" {
			t.Errorf("optional glossary mode = %q, want auto", modes["glossary"])
		}
	})

	t.Run("a step pointing at another organization is refused", func(t *testing.T) {
		// The intruder's own repository, in their own organization.
		intruderToken := sut.registerOrg(t, "intruder-onb")
		foreignRepo := sut.createRepository(t, intruderToken, hugeURL)

		resp := sut.do(t, http.MethodPut, "/api/v1/onboarding/flows/"+flow.ID+"/steps", adminToken, map[string]any{
			"steps": []map[string]any{
				{"kind": "repository", "title": "Roubado", "config": map[string]any{"repository_id": foreignRepo.ID}},
			},
		})
		// Walking a flow renders whatever a step points at, so this would be a
		// data leak rather than a broken link.
		if resp.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.status, resp.body)
		}
		if len(sut.mustFlowSteps(t, adminToken, flow.ID)) != 9 {
			t.Error("the rejected save changed the stored steps")
		}
	})

	var inviteToken string
	t.Run("an invite carries the onboarding", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodPost, "/api/v1/organizations/invites", adminToken, map[string]any{
			"email":              "novato@e2e.example",
			"role":               "developer",
			"onboarding_flow_id": flow.ID,
		}, http.StatusCreated)
		var invite struct {
			Token  string `json:"token"`
			Invite struct {
				OnboardingFlowID *string `json:"onboarding_flow_id"`
			} `json:"invite"`
		}
		resp.decode(t, &invite)

		if invite.Token == "" {
			t.Fatal("the invite returned no token")
		}
		if invite.Invite.OnboardingFlowID == nil || *invite.Invite.OnboardingFlowID != flow.ID {
			t.Errorf("invite flow = %v, want %q", invite.Invite.OnboardingFlowID, flow.ID)
		}
		inviteToken = invite.Token
	})

	var newcomerToken string
	t.Run("accepting the invite assigns the onboarding", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email":        "novato@e2e.example",
			"password":     "E2ePassw0rd!",
			"full_name":    "Novato E2E",
			"invite_token": inviteToken,
		}, http.StatusCreated)
		var token tokenResponse
		resp.decode(t, &token)
		newcomerToken = token.AccessToken

		runs := sut.myOnboarding(t, newcomerToken)
		if len(runs) != 1 {
			t.Fatalf("got %d onboardings, want the invite's one", len(runs))
		}
		if runs[0].FlowName != "Dev Backend" || runs[0].StepsTotal != 9 {
			t.Errorf("run = %+v, want the assigned flow with 9 steps", runs[0])
		}
		// Nothing done yet, and every required step still pending.
		if runs[0].StepsDone != 0 || runs[0].RequiredRemaining != 8 {
			t.Errorf("run = %+v, want 0 done and 8 required pending", runs[0])
		}
	})

	t.Run("every referential step arrives resolved from live data", func(t *testing.T) {
		runs := sut.myOnboarding(t, newcomerToken)
		byKind := map[string]int{}
		for i, step := range runs[0].Steps {
			byKind[step.Kind] = i
		}

		repoStep := runs[0].Steps[byKind["repository"]]
		if repoStep.Resolved == nil || repoStep.Resolved.Repository == nil {
			t.Fatalf("repository step = %+v, want it resolved", repoStep)
		}
		if repoStep.Resolved.Repository.Name != fakegitlab.RunnerPath {
			t.Errorf("repository name = %q, want the catalog's", repoStep.Resolved.Repository.Name)
		}

		teamStep := runs[0].Steps[byKind["team"]]
		if teamStep.Resolved == nil || teamStep.Resolved.Team == nil {
			t.Fatalf("team step = %+v, want it resolved", teamStep)
		}
		if teamStep.Resolved.Team.Name != "Plataforma" {
			t.Errorf("team = %+v, want Plataforma", teamStep.Resolved.Team)
		}
		// "What does this team answer for" — the read that had no endpoint
		// before this feature.
		if len(teamStep.Resolved.Team.Repositories) != 1 || teamStep.Resolved.Team.Repositories[0].ID != repo.ID {
			t.Errorf("team repositories = %+v, want the one it owns", teamStep.Resolved.Team.Repositories)
		}

		glossaryStep := runs[0].Steps[byKind["glossary"]]
		if glossaryStep.Resolved == nil || len(glossaryStep.Resolved.Terms) != 1 {
			t.Errorf("glossary step = %+v, want the organization's term", glossaryStep)
		}

		contactStep := runs[0].Steps[byKind["contacts"]]
		if contactStep.Resolved == nil || len(contactStep.Resolved.People) != 1 {
			t.Fatalf("contacts step = %+v, want the person resolved", contactStep)
		}
		if contactStep.Resolved.People[0].FullName == "" || contactStep.Resolved.People[0].Area != "Acesso" {
			t.Errorf("contact = %+v, want a named person and their area", contactStep.Resolved.People[0])
		}

		verifiedStep := runs[0].Steps[byKind["verified"]]
		if verifiedStep.Resolved == nil || verifiedStep.Resolved.Verification == nil {
			t.Fatalf("verified step = %+v, want the claim described", verifiedStep)
		}
		if verifiedStep.Resolved.Verification.Description == "" {
			t.Error("a verified step must say what it will check")
		}
	})

	t.Run("verification answers honestly before and after the fact", func(t *testing.T) {
		runs := sut.myOnboarding(t, newcomerToken)
		var stepID string
		for _, step := range runs[0].Steps {
			if step.Kind == "verified" {
				stepID = step.ID
			}
		}

		resp := sut.mustDo(t, http.MethodPost, "/api/v1/onboarding/me/steps/"+stepID+"/verify", newcomerToken, nil, http.StatusOK)
		var before verificationResponse
		resp.decode(t, &before)
		if before.Passed {
			t.Error("passed before the newcomer was on any team")
		}
		// Even a negative explains itself.
		if before.How == "" {
			t.Error("the verification explained nothing")
		}

		// Put them on the team, which is exactly what the step checks.
		newcomerID := ""
		membersResp := sut.mustDo(t, http.MethodGet, "/api/v1/organizations/members", adminToken, nil, http.StatusOK)
		var updatedMembers struct {
			Items []struct {
				UserID string `json:"user_id"`
				Email  string `json:"email"`
			} `json:"items"`
		}
		membersResp.decode(t, &updatedMembers)
		for _, member := range updatedMembers.Items {
			if member.Email == "novato@e2e.example" {
				newcomerID = member.UserID
			}
		}
		if newcomerID == "" {
			t.Fatal("the newcomer is not listed as a member")
		}
		sut.mustDo(t, http.MethodPost, "/api/v1/teams/"+team.ID+"/members", adminToken, map[string]any{
			"user_id": newcomerID,
		}, http.StatusNoContent)

		resp = sut.mustDo(t, http.MethodPost, "/api/v1/onboarding/me/steps/"+stepID+"/verify", newcomerToken, nil, http.StatusOK)
		var after verificationResponse
		resp.decode(t, &after)
		if !after.Passed || after.Pending {
			t.Fatalf("verification = %+v, want a pass once the fact is true", after)
		}

		// A pass is recorded, so the step counts as done without anyone marking
		// it by hand.
		runs = sut.myOnboarding(t, newcomerToken)
		for _, step := range runs[0].Steps {
			if step.ID == stepID && step.Status != "done" {
				t.Errorf("verified step status = %q, want done", step.Status)
			}
		}
	})

	t.Run("marking the required steps completes the flow", func(t *testing.T) {
		runs := sut.myOnboarding(t, newcomerToken)
		var last runResponse
		for _, step := range runs[0].Steps {
			if step.Status != "" {
				continue
			}
			status := "done"
			if step.Kind == "glossary" {
				// Optional: skipped on purpose, to prove an optional step does
				// not hold the flow open.
				status = "skipped"
			}
			resp := sut.mustDo(t, http.MethodPost, "/api/v1/onboarding/me/steps/"+step.ID, newcomerToken, map[string]any{
				"status": status,
			}, http.StatusOK)
			resp.decode(t, &last)
		}

		if last.RequiredRemaining != 0 {
			t.Fatalf("required remaining = %d, want 0", last.RequiredRemaining)
		}
		// Completion is derived, never declared by the client.
		if last.Status != "completed" {
			t.Fatalf("status = %q, want completed", last.Status)
		}
	})

	t.Run("the newcomer can say what was missing", func(t *testing.T) {
		runs := sut.myOnboarding(t, newcomerToken)
		sut.mustDo(t, http.MethodPost,
			"/api/v1/onboarding/me/assignments/"+runs[0].AssignmentID+"/feedback",
			newcomerToken, map[string]any{"feedback": "faltou explicar como pedir acesso ao staging"},
			http.StatusNoContent)

		runs = sut.myOnboarding(t, newcomerToken)
		if runs[0].Feedback == "" {
			t.Error("the feedback was not stored")
		}
	})

	t.Run("the admin dashboard shows the walk and the feedback", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodGet, "/api/v1/onboarding/assignments", adminToken, nil, http.StatusOK)
		var list struct {
			Items []struct {
				UserName   string `json:"user_name"`
				UserEmail  string `json:"user_email"`
				FlowName   string `json:"flow_name"`
				Status     string `json:"status"`
				StepsDone  int    `json:"steps_done"`
				StepsTotal int    `json:"steps_total"`
				Feedback   string `json:"feedback"`
			} `json:"items"`
		}
		resp.decode(t, &list)

		if len(list.Items) != 1 {
			t.Fatalf("dashboard has %d rows, want 1", len(list.Items))
		}
		row := list.Items[0]
		if row.Status != "completed" || row.StepsTotal != 9 {
			t.Errorf("row = %+v, want a completed 9-step walk", row)
		}
		if row.UserEmail != "novato@e2e.example" {
			t.Errorf("user = %q, want the newcomer identified", row.UserEmail)
		}
		if row.Feedback == "" {
			t.Error("the dashboard does not surface the feedback")
		}
	})

	t.Run("a deleted repository leaves the step renderable", func(t *testing.T) {
		sut.mustDo(t, http.MethodDelete, "/api/v1/repositories/"+repo.ID, adminToken, nil, http.StatusNoContent)

		runs := sut.myOnboarding(t, newcomerToken)
		if len(runs) != 1 {
			t.Fatalf("the onboarding disappeared with the repository")
		}
		for _, step := range runs[0].Steps {
			if step.Kind != "repository" {
				continue
			}
			// The failure this prevents: a 500 that stops the whole flow because
			// somebody archived a repository.
			if step.Unavailable == "" {
				t.Errorf("repository step = %+v, want an explanation in place of the entity", step)
			}
			if step.Resolved != nil && step.Resolved.Repository != nil {
				t.Error("a deleted repository was still resolved")
			}
		}
	})

	t.Run("a dangling reference blocks the save until the step is removed", func(t *testing.T) {
		// The repository was deleted in the previous subtest, so the flow still
		// holds a step pointing at nothing. Reading it is fine — that is what
		// `unavailable` is for — but writing it back is not: the same validation
		// that keeps a foreign reference out also refuses a dead one.
		existing := sut.mustFlowSteps(t, adminToken, flow.ID)
		resp := sut.do(t, http.MethodPut, "/api/v1/onboarding/flows/"+flow.ID+"/steps", adminToken, map[string]any{
			"steps": stepPayload(existing),
		})
		if resp.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 while a step points at a deleted repository\nbody: %s", resp.status, resp.body)
		}
	})

	t.Run("a required step added later reopens a finished flow", func(t *testing.T) {
		existing := sut.mustFlowSteps(t, adminToken, flow.ID)
		payload := make([]map[string]any, 0, len(existing)+1)
		for _, step := range stepPayload(existing) {
			// Drop the step whose repository is gone, which is what the admin
			// would do after the message above.
			if step["kind"] == "repository" {
				continue
			}
			payload = append(payload, step)
		}
		payload = append(payload, map[string]any{
			"kind": "markdown", "title": "Política nova", "body": "# Leia isto", "is_required": true,
		})

		sut.mustDo(t, http.MethodPut, "/api/v1/onboarding/flows/"+flow.ID+"/steps", adminToken, map[string]any{
			"steps": payload,
		}, http.StatusOK)

		runs := sut.myOnboarding(t, newcomerToken)
		if runs[0].RequiredRemaining != 1 {
			t.Errorf("required remaining = %d, want the new step pending", runs[0].RequiredRemaining)
		}
		// The point of passing ids back: the earlier progress survives the edit.
		if runs[0].StepsDone < 6 {
			t.Errorf("steps done = %d, want the earlier progress kept", runs[0].StepsDone)
		}
	})
}

// stepPayload turns stored steps back into a save payload, carrying each id so
// the server updates those rows in place instead of recreating them — which is
// what keeps the progress pointing at them alive.
func stepPayload(steps []storedStep) []map[string]any {
	payload := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		payload = append(payload, map[string]any{
			"id": step.ID, "kind": step.Kind, "title": step.Title,
			"body": step.Body, "config": step.Config, "is_required": step.IsRequired,
		})
	}
	return payload
}

func TestOnboardingWithoutAFlowIsNotAnError(t *testing.T) {
	// An organization that never wrote an onboarding. Joining it must work, and
	// the runner must render "nothing assigned" rather than fail.
	adminToken := sut.registerOrg(t, "no-onboarding")

	resp := sut.mustDo(t, http.MethodPost, "/api/v1/organizations/invites", adminToken, map[string]any{
		"email": "sozinho@e2e.example",
		"role":  "developer",
	}, http.StatusCreated)
	var invite struct {
		Token string `json:"token"`
	}
	resp.decode(t, &invite)

	resp = sut.mustDo(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":        "sozinho@e2e.example",
		"password":     "E2ePassw0rd!",
		"full_name":    "Sozinho",
		"invite_token": invite.Token,
	}, http.StatusCreated)
	var token tokenResponse
	resp.decode(t, &token)

	runs := sut.myOnboarding(t, token.AccessToken)
	if len(runs) != 0 {
		t.Fatalf("got %d onboardings, want none", len(runs))
	}
}

func TestOnboardingConfigurationIsAdminOnly(t *testing.T) {
	adminToken := sut.registerOrg(t, "onboarding-roles")

	resp := sut.mustDo(t, http.MethodPost, "/api/v1/organizations/invites", adminToken, map[string]any{
		"email": "dev@e2e.example",
		"role":  "developer",
	}, http.StatusCreated)
	var invite struct {
		Token string `json:"token"`
	}
	resp.decode(t, &invite)

	resp = sut.mustDo(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":        "dev@e2e.example",
		"password":     "E2ePassw0rd!",
		"full_name":    "Dev",
		"invite_token": invite.Token,
	}, http.StatusCreated)
	var devToken tokenResponse
	resp.decode(t, &devToken)

	// A developer reads flows — the runner needs to — but composing them is an
	// admin concern, like membership and organization config.
	sut.mustDo(t, http.MethodGet, "/api/v1/onboarding/flows", devToken.AccessToken, nil, http.StatusOK)

	forbidden := sut.do(t, http.MethodPost, "/api/v1/onboarding/flows", devToken.AccessToken, map[string]any{
		"name": "Meu fluxo",
	})
	if forbidden.status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403\nbody: %s", forbidden.status, forbidden.body)
	}

	// And the progress dashboard is admin-only too.
	dashboard := sut.do(t, http.MethodGet, "/api/v1/onboarding/assignments", devToken.AccessToken, nil)
	if dashboard.status != http.StatusForbidden {
		t.Fatalf("dashboard status = %d, want 403", dashboard.status)
	}
}

// mustFlowSteps reads a flow's steps, which several subtests need in order to
// pass ids back on a save.
type storedStep struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Body       string         `json:"body"`
	Config     map[string]any `json:"config"`
	IsRequired bool           `json:"is_required"`
}

func (s *stack) mustFlowSteps(t *testing.T, token, flowID string) []storedStep {
	t.Helper()
	resp := s.mustDo(t, http.MethodGet, fmt.Sprintf("/api/v1/onboarding/flows/%s", flowID), token, nil, http.StatusOK)
	var flow struct {
		Steps []storedStep `json:"steps"`
	}
	resp.decode(t, &flow)
	return flow.Steps
}
