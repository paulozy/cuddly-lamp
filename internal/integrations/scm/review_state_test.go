package scm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
)

type reviewClient struct {
	github.ClientInterface
	reviews []github.Review
	err     error
}

func (c *reviewClient) ListReviews(_ context.Context, _, _ string, _ int64) ([]github.Review, error) {
	return c.reviews, c.err
}

func review(login, state string) github.Review {
	return github.Review{User: github.User{Login: login}, State: state}
}

// The reduction rules are where this is easy to get wrong: a person can review
// many times, and only their latest position counts.
func TestGitHubProvider_ReducesReviewHistoryToACurrentVerdict(t *testing.T) {
	tests := []struct {
		name                   string
		reviews                []github.Review
		wantDecision           string
		wantApproved           []string
		wantChangesRequestedBy []string
	}{
		{
			name:         "nobody reviewed",
			reviews:      nil,
			wantDecision: "",
		},
		{
			name:         "a single approval",
			reviews:      []github.Review{review("julia.r", "APPROVED")},
			wantDecision: ReviewDecisionApproved,
			wantApproved: []string{"julia.r"},
		},
		{
			name:                   "changes requested outranks an approval by someone else",
			reviews:                []github.Review{review("julia.r", "APPROVED"), review("caio", "CHANGES_REQUESTED")},
			wantDecision:           ReviewDecisionChangesRequested,
			wantApproved:           []string{"julia.r"},
			wantChangesRequestedBy: []string{"caio"},
		},
		{
			name:         "a reviewer's later approval replaces their objection",
			reviews:      []github.Review{review("caio", "CHANGES_REQUESTED"), review("caio", "APPROVED")},
			wantDecision: ReviewDecisionApproved,
			wantApproved: []string{"caio"},
		},
		{
			name:                   "a reviewer's later objection replaces their approval",
			reviews:                []github.Review{review("caio", "APPROVED"), review("caio", "CHANGES_REQUESTED")},
			wantDecision:           ReviewDecisionChangesRequested,
			wantChangesRequestedBy: []string{"caio"},
		},
		{
			name:         "a comment does not displace an approval",
			reviews:      []github.Review{review("julia.r", "APPROVED"), review("caio", "COMMENTED")},
			wantDecision: ReviewDecisionApproved,
			wantApproved: []string{"julia.r"},
		},
		{
			name:         "a comment alone is reviewed without a verdict",
			reviews:      []github.Review{review("caio", "COMMENTED")},
			wantDecision: ReviewDecisionCommented,
		},
		{
			name:         "a dismissed review is no longer a position",
			reviews:      []github.Review{review("julia.r", "APPROVED"), review("julia.r", "DISMISSED")},
			wantDecision: "",
		},
		{
			name:         "reviewers are sorted so the order is stable",
			reviews:      []github.Review{review("zeca", "APPROVED"), review("ana", "APPROVED")},
			wantDecision: ReviewDecisionApproved,
			wantApproved: []string{"ana", "zeca"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubProviderWithClient(&reviewClient{reviews: tt.reviews}, "token")

			state, err := provider.GetChangeRequestReviews(context.Background(), RepoRef{Namespace: "o", Name: "r"}, 42)
			if err != nil {
				t.Fatalf("GetChangeRequestReviews: %v", err)
			}
			if state.Decision != tt.wantDecision {
				t.Errorf("Decision = %q, want %q", state.Decision, tt.wantDecision)
			}
			assertLogins(t, "ApprovedBy", state.ApprovedBy, tt.wantApproved)
			assertLogins(t, "ChangesRequestedBy", state.ChangesRequestedBy, tt.wantChangesRequestedBy)
		})
	}
}

func assertLogins(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", field, i, got[i], want[i])
		}
	}
}

func TestGitHubProvider_ReviewStateTranslatesErrors(t *testing.T) {
	provider := NewGitHubProviderWithClient(&reviewClient{err: github.ErrNotFound}, "token")

	if _, err := provider.GetChangeRequestReviews(context.Background(), RepoRef{Namespace: "o", Name: "r"}, 42); err != ErrNotFound {
		t.Errorf("err = %v, want the canonical ErrNotFound", err)
	}
}

// GitLab reports approvals only. Its ChangesRequestedBy must stay empty rather
// than being synthesized from something else.
func TestGitLabProvider_ReportsApprovalsAndNeverChangesRequested(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath, not Path: the nested-group encoding is the point, and
		// Path would show it already decoded.
		gotPath = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(map[string]any{
			"approvals_required": 1,
			"approvals_left":     0,
			"approved_by": []map[string]any{
				{"user": map[string]string{"username": "zeca"}},
				{"user": map[string]string{"username": "ana"}},
			},
		})
	}))
	defer srv.Close()

	state, err := newGitLabProvider(srv).GetChangeRequestReviews(context.Background(), RepoRef{Namespace: "g", Name: "p"}, 7)
	if err != nil {
		t.Fatalf("GetChangeRequestReviews: %v", err)
	}
	if state.Decision != ReviewDecisionApproved {
		t.Errorf("Decision = %q, want %q", state.Decision, ReviewDecisionApproved)
	}
	assertLogins(t, "ApprovedBy", state.ApprovedBy, []string{"ana", "zeca"})
	if len(state.ChangesRequestedBy) != 0 {
		t.Errorf("ChangesRequestedBy = %v, want empty — GitLab has no such state", state.ChangesRequestedBy)
	}
	if gotPath != "/projects/g%2Fp/merge_requests/7/approvals" {
		t.Errorf("path = %q, want the URL-encoded project path", gotPath)
	}
}

func TestGitLabProvider_NoApprovalsMeansNoVerdict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"approvals_left": 1, "approved_by": []any{}})
	}))
	defer srv.Close()

	state, err := newGitLabProvider(srv).GetChangeRequestReviews(context.Background(), RepoRef{Namespace: "g", Name: "p"}, 7)
	if err != nil {
		t.Fatalf("GetChangeRequestReviews: %v", err)
	}
	if state.Decision != "" {
		t.Errorf("Decision = %q, want empty", state.Decision)
	}
}
