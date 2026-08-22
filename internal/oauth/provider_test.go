package oauth

import "testing"

// The profile payloads both providers return carry the login, and it used to be
// dropped on the floor: OAuthUserInfo only had the numeric id. Change requests
// identify their author by login, so a verified onboarding step has no way to
// recognise the person without it.

func TestUserInfoFromGitHub(t *testing.T) {
	got := userInfoFromGitHub(githubUser{
		ID:    12345,
		Login: "octocat",
		Email: "octocat@example.com",
		Name:  "The Octocat",
	})

	if got.ProviderUserID != "12345" {
		t.Errorf("ProviderUserID = %q, want 12345", got.ProviderUserID)
	}
	if got.Username != "octocat" {
		t.Errorf("Username = %q, want octocat", got.Username)
	}
	if got.Email != "octocat@example.com" || got.Name != "The Octocat" {
		t.Errorf("info = %+v, want the profile's email and name", got)
	}
}

func TestUserInfoFromGitHub_EmailFallbackKeepsTheLogin(t *testing.T) {
	// GitHub hides the address when the user keeps it private; the synthesized
	// placeholder must not cost us the login as well.
	got := userInfoFromGitHub(githubUser{ID: 777, Login: "private-person", Name: "P"})

	if got.Email != "github_777@noreply.github.com" {
		t.Errorf("Email = %q, want the noreply placeholder", got.Email)
	}
	if got.Username != "private-person" {
		t.Errorf("Username = %q, want private-person", got.Username)
	}
}

func TestUserInfoFromGitLab(t *testing.T) {
	got := userInfoFromGitLab(gitlabUser{
		ID:       98765,
		Username: "tanuki",
		Email:    "tanuki@example.com",
		Name:     "The Tanuki",
	})

	if got.ProviderUserID != "98765" {
		t.Errorf("ProviderUserID = %q, want 98765", got.ProviderUserID)
	}
	// GitLab calls it `username` where GitHub calls it `login`; both land here.
	if got.Username != "tanuki" {
		t.Errorf("Username = %q, want tanuki", got.Username)
	}
	if got.Email != "tanuki@example.com" || got.Name != "The Tanuki" {
		t.Errorf("info = %+v, want the profile's email and name", got)
	}
}

func TestUserInfoFromGitLab_EmailFallbackKeepsTheUsername(t *testing.T) {
	got := userInfoFromGitLab(gitlabUser{ID: 42, Username: "quiet", Name: "Q"})

	if got.Email != "gitlab_42@noreply.gitlab.com" {
		t.Errorf("Email = %q, want the noreply placeholder", got.Email)
	}
	if got.Username != "quiet" {
		t.Errorf("Username = %q, want quiet", got.Username)
	}
}
