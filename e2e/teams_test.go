//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type teamResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Source      string `json:"source"`
	MemberCount int    `json:"member_count"`
}

// An organization must be able to hold more than one team.
//
// This is a regression test for a real failure: `idx_teams_org_external` was
// declared `WHERE external_id IS NOT NULL`, meaning to cover only imported
// teams — but a locally created team stores an empty string there rather than
// NULL, and an empty string is not NULL. Every local team therefore landed in
// the index under the same (empty, empty) pair, so the *second* team an
// organization created died on a unique violation that the API reported as a
// bare 500.
//
// A unit test could not have caught it: the constraint only exists in the
// database.
func TestAnOrganizationCanHoldSeveralTeams(t *testing.T) {
	token := sut.registerOrg(t, "several-teams")

	names := []string{"Plataforma", "Pagamentos", "Inovação & Dados"}
	created := make([]teamResponse, 0, len(names))

	for _, name := range names {
		resp := sut.mustDo(t, http.MethodPost, "/api/v1/teams", token,
			map[string]any{"name": name}, http.StatusCreated)

		var team teamResponse
		if err := json.Unmarshal([]byte(resp.body), &team); err != nil {
			t.Fatalf("decode team %q: %v\nbody: %s", name, err, resp.body)
		}
		if team.Name != name {
			t.Errorf("name = %q, want %q", team.Name, name)
		}
		// The creator joins as lead, so a fresh team is never empty.
		if team.MemberCount != 1 {
			t.Errorf("member_count = %d, want 1 for %q", team.MemberCount, name)
		}
		created = append(created, team)
	}

	// Slugs must stay distinct — including the accented name, which slugifies.
	seen := map[string]bool{}
	for _, team := range created {
		if team.Slug == "" {
			t.Errorf("team %q got an empty slug", team.Name)
		}
		if seen[team.Slug] {
			t.Errorf("slug %q was issued twice", team.Slug)
		}
		seen[team.Slug] = true
	}

	resp := sut.mustDo(t, http.MethodGet, "/api/v1/teams", token, nil, http.StatusOK)
	var list struct {
		Items []teamResponse `json:"items"`
		Total int            `json:"total"`
	}
	json.Unmarshal([]byte(resp.body), &list)
	if list.Total != len(names) {
		t.Errorf("total = %d, want %d\nbody: %s", list.Total, len(names), resp.body)
	}
}

// Reusing a name is a conflict the caller can act on, not an internal error.
// The two used to be indistinguishable from the outside — both were 500s.
func TestDuplicateTeamNameIsAConflict(t *testing.T) {
	token := sut.registerOrg(t, "duplicate-team")

	sut.mustDo(t, http.MethodPost, "/api/v1/teams", token,
		map[string]any{"name": "Plataforma"}, http.StatusCreated)

	resp := sut.mustDo(t, http.MethodPost, "/api/v1/teams", token,
		map[string]any{"name": "Plataforma"}, http.StatusConflict)

	if !strings.Contains(string(resp.body), "team_already_exists") {
		t.Errorf("body = %s, want the team_already_exists code", resp.body)
	}
}

// Renaming is what the settings editor calls; it had no coverage at all.
func TestTeamCanBeRenamed(t *testing.T) {
	token := sut.registerOrg(t, "rename-team")

	resp := sut.mustDo(t, http.MethodPost, "/api/v1/teams", token,
		map[string]any{"name": "Plataforma"}, http.StatusCreated)
	var team teamResponse
	json.Unmarshal([]byte(resp.body), &team)

	renamed := sut.mustDo(t, http.MethodPatch, "/api/v1/teams/"+team.ID, token,
		map[string]any{"name": "Plataforma Core"}, http.StatusOK)

	var after teamResponse
	json.Unmarshal([]byte(renamed.body), &after)
	if after.Name != "Plataforma Core" {
		t.Errorf("name = %q, want Plataforma Core", after.Name)
	}
}
