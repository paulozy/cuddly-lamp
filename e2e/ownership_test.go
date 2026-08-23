//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
)

// What team ownership does — and, just as importantly, what it does not.
//
// The platform is a catalog: every member of an organization sees every
// repository in it, because discovery across the whole organization is the
// point. Ownership answers "who is accountable, and who may change this",
// not "who may look".
//
// So a developer on the team that owns one repository can write that one and
// only that one, while still reading the entire catalog. This test pins both
// halves, because getting either wrong is a different serious bug: scoping
// reads would gut the catalog, and unscoping writes would let any developer
// edit another team's service.
func TestTeamOwnershipGatesWritesAndNotReads(t *testing.T) {
	admin := sut.registerOrg(t, "ownership")
	configureGitLabToken(t, admin)

	ours := sut.createRepository(t, admin, runnerURL)
	theirs := sut.createRepository(t, admin, hugeURL)

	// Two teams, each accountable for one repository.
	ourTeam := createTeam(t, admin, "Plataforma")
	theirTeam := createTeam(t, admin, "Pagamentos")
	setOwner(t, admin, ours.ID, ourTeam)
	setOwner(t, admin, theirs.ID, theirTeam)

	dev := inviteDeveloper(t, admin, "owner-dev@e2e.example")
	addTeamMember(t, admin, ourTeam, dev.userID)

	t.Run("the developer reads the whole catalog, not just their team's", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodGet, "/api/v1/repositories", dev.token, nil, http.StatusOK)
		// The list endpoint keys on `items`; the frontend's BFF renames it to
		// `repositories` in normalizeRepositoryList.
		var list struct {
			Items []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
			Total int `json:"total"`
		}
		resp.decode(t, &list)

		if list.Total != 2 {
			t.Fatalf("total = %d, want 2 — the catalog is organization-wide\nbody: %s", list.Total, resp.body)
		}
		seen := map[string]bool{}
		for _, repo := range list.Items {
			seen[repo.ID] = true
		}
		if !seen[ours.ID] || !seen[theirs.ID] {
			t.Errorf("catalog = %+v, want both repositories", list.Items)
		}
	})

	// What powers the catalog's "Meus times" filter: the team list tells each
	// caller which teams are theirs, so the client needs no request per team.
	t.Run("the team list marks the caller's own teams", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodGet, "/api/v1/teams", dev.token, nil, http.StatusOK)
		var list struct {
			Items []struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				ViewerIsMember bool   `json:"viewer_is_member"`
			} `json:"items"`
		}
		resp.decode(t, &list)

		if len(list.Items) != 2 {
			t.Fatalf("got %d teams, want 2\nbody: %s", len(list.Items), resp.body)
		}
		for _, team := range list.Items {
			want := team.ID == ourTeam
			if team.ViewerIsMember != want {
				t.Errorf("team %q viewer_is_member = %v, want %v", team.Name, team.ViewerIsMember, want)
			}
		}
	})

	// The same list, read by someone else, must answer differently — the flag is
	// per caller, not a property of the team.
	t.Run("and answers differently for a different caller", func(t *testing.T) {
		resp := sut.mustDo(t, http.MethodGet, "/api/v1/teams", admin, nil, http.StatusOK)
		var list struct {
			Items []struct {
				ID             string `json:"id"`
				ViewerIsMember bool   `json:"viewer_is_member"`
			} `json:"items"`
		}
		resp.decode(t, &list)

		for _, team := range list.Items {
			// The admin created both teams and joined each as lead.
			if !team.ViewerIsMember {
				t.Errorf("team %s viewer_is_member = false for its creator", team.ID)
			}
		}
	})

	t.Run("reading another team's repository directly is allowed too", func(t *testing.T) {
		sut.mustDo(t, http.MethodGet, "/api/v1/repositories/"+theirs.ID, dev.token, nil, http.StatusOK)
	})

	t.Run("the developer may edit the repository their team owns", func(t *testing.T) {
		sut.mustDo(t, http.MethodPut, "/api/v1/repositories/"+ours.ID, dev.token,
			map[string]any{"description": "cuidado por Plataforma"}, http.StatusOK)
	})

	// The rule that makes ownership worth having: role alone would let this
	// through, since the caller is a developer either way.
	t.Run("but not one owned by another team", func(t *testing.T) {
		resp := sut.do(t, http.MethodPut, "/api/v1/repositories/"+theirs.ID, dev.token,
			map[string]any{"description": "não deveria passar"})
		if resp.status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403\nbody: %s", resp.status, resp.body)
		}
	})

	t.Run("nor an unowned one", func(t *testing.T) {
		setOwner(t, admin, ours.ID, "")
		resp := sut.do(t, http.MethodPut, "/api/v1/repositories/"+ours.ID, dev.token,
			map[string]any{"description": "sem dono não é de todos"})
		if resp.status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 — claiming an unowned repository is a deliberate act\nbody: %s",
				resp.status, resp.body)
		}
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func createTeam(t *testing.T, token, name string) string {
	t.Helper()
	resp := sut.mustDo(t, http.MethodPost, "/api/v1/teams", token,
		map[string]any{"name": name}, http.StatusCreated)
	var team teamResponse
	if err := json.Unmarshal(resp.body, &team); err != nil {
		t.Fatalf("decode team: %v", err)
	}
	return team.ID
}

// setOwner assigns a team, or clears the owner when teamID is empty.
func setOwner(t *testing.T, token, repoID, teamID string) {
	t.Helper()
	var payload map[string]any
	if teamID == "" {
		payload = map[string]any{"team_id": nil}
	} else {
		payload = map[string]any{"team_id": teamID}
	}
	sut.mustDo(t, http.MethodPut, "/api/v1/repositories/"+repoID+"/owner", token, payload, http.StatusNoContent)
}

func addTeamMember(t *testing.T, token, teamID, userID string) {
	t.Helper()
	sut.mustDo(t, http.MethodPost, "/api/v1/teams/"+teamID+"/members", token,
		map[string]any{"user_id": userID}, http.StatusNoContent)
}

type invitedUser struct {
	token  string
	userID string
}

func inviteDeveloper(t *testing.T, adminToken, email string) invitedUser {
	t.Helper()
	resp := sut.mustDo(t, http.MethodPost, "/api/v1/organizations/invites", adminToken,
		map[string]any{"email": email, "role": "developer"}, http.StatusCreated)
	var invite struct {
		Token string `json:"token"`
	}
	resp.decode(t, &invite)

	resp = sut.mustDo(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":        email,
		"password":     "E2ePassw0rd!",
		"full_name":    "Owner Dev",
		"invite_token": invite.Token,
	}, http.StatusCreated)
	var token tokenResponse
	resp.decode(t, &token)

	me := sut.mustDo(t, http.MethodGet, "/api/v1/users/me", token.AccessToken, nil, http.StatusOK)
	var user struct {
		ID string `json:"id"`
	}
	me.decode(t, &user)
	if user.ID == "" {
		t.Fatalf("users/me returned no id: %s", me.body)
	}

	return invitedUser{token: token.AccessToken, userID: user.ID}
}
