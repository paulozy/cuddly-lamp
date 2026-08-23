package scm

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/gitlab"
)

// TestTranslateGitHubErr_SeparatesRefusalFromOutage is the heart of the fix.
// Every row below used to collapse into one unclassified error that handlers
// reported as 503, which told callers to retry actions that could never
// succeed.
func TestTranslateGitHubErr_SeparatesRefusalFromOutage(t *testing.T) {
	tests := []struct {
		name           string
		apiErr         *github.APIError
		wantRejected   bool
		wantSelfReview bool
		wantReason     string
		wantMessage    string
	}{
		{
			name: "self-approval is named",
			apiErr: &github.APIError{
				Status:  http.StatusUnprocessableEntity,
				Message: "Unprocessable Entity",
				Errors:  []string{"Review Can not approve your own pull request"},
			},
			wantRejected:   true,
			wantSelfReview: true,
			wantReason:     ReasonSelfReview,
			wantMessage:    "Review Can not approve your own pull request",
		},
		{
			name: "an already-reviewed change request is named",
			apiErr: &github.APIError{
				Status:  http.StatusUnprocessableEntity,
				Message: "Unprocessable Entity",
				Errors:  []string{"Review has already approved this pull request"},
			},
			wantRejected: true,
			wantReason:   ReasonAlreadyReviewed,
		},
		{
			name: "an unrecognized refusal is still a refusal",
			apiErr: &github.APIError{
				Status:  http.StatusUnprocessableEntity,
				Message: "Validation Failed",
				Errors:  []string{"No commits between main and feature"},
			},
			wantRejected: true,
			wantReason:   "",
			wantMessage:  "No commits between main and feature",
		},
		{
			name: "a permission refusal is a refusal, not an outage",
			apiErr: &github.APIError{
				Status:  http.StatusForbidden,
				Message: "Resource not accessible by integration",
			},
			wantRejected: true,
			wantMessage:  "Resource not accessible by integration",
		},
		{
			name: "a server error stays an outage",
			apiErr: &github.APIError{
				Status:  http.StatusBadGateway,
				Message: "Bad Gateway",
			},
			wantRejected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := translateGitHubErr(tt.apiErr)

			if got := errors.Is(err, ErrProviderRejected); got != tt.wantRejected {
				t.Fatalf("errors.Is(ErrProviderRejected) = %v, want %v (err = %v)", got, tt.wantRejected, err)
			}
			if got := errors.Is(err, ErrSelfReview); got != tt.wantSelfReview {
				t.Errorf("errors.Is(ErrSelfReview) = %v, want %v", got, tt.wantSelfReview)
			}
			if !tt.wantRejected {
				return
			}

			var providerErr *ProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("err = %v (%T), want *ProviderError", err, err)
			}
			if providerErr.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", providerErr.Reason, tt.wantReason)
			}
			if providerErr.Status != tt.apiErr.Status {
				t.Errorf("Status = %d, want %d", providerErr.Status, tt.apiErr.Status)
			}
			if providerErr.Provider != "github" {
				t.Errorf("Provider = %q, want github", providerErr.Provider)
			}
			if tt.wantMessage != "" && providerErr.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", providerErr.Message, tt.wantMessage)
			}
		})
	}
}

// A refusal must never degrade into an outage just because the wording changed.
// This is the safety property that makes prose matching acceptable at all.
func TestTranslateGitHubErr_UnknownWordingStaysARefusal(t *testing.T) {
	err := translateGitHubErr(&github.APIError{
		Status:  http.StatusUnprocessableEntity,
		Message: "Unprocessable Entity",
		Errors:  []string{"Some phrasing GitHub has not used before"},
	})

	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("err = %v, want a rejection", err)
	}
	if errors.Is(err, ErrSelfReview) {
		t.Error("unrecognized wording must not be reported as self review")
	}

	var providerErr *ProviderError
	errors.As(err, &providerErr)
	if !strings.Contains(providerErr.Message, "not used before") {
		t.Errorf("Message = %q, want the host's own words preserved", providerErr.Message)
	}
}

// The existing sentinels must keep winning over the new classification, or
// every caller doing errors.Is(err, ErrNotFound) breaks.
func TestTranslateGitHubErr_KeepsExistingSentinels(t *testing.T) {
	tests := []struct {
		in   error
		want error
	}{
		{in: nil, want: nil},
		{in: github.ErrNotFound, want: ErrNotFound},
		{in: github.ErrUnauthorized, want: ErrUnauthorized},
		{in: github.ErrRateLimited, want: ErrRateLimited},
	}

	for _, tt := range tests {
		got := translateGitHubErr(tt.in)
		if tt.want == nil {
			if got != nil {
				t.Errorf("translateGitHubErr(nil) = %v, want nil", got)
			}
			continue
		}
		if !errors.Is(got, tt.want) {
			t.Errorf("translateGitHubErr(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// GitLab gets the status split but no reason inference — see
// classifyGitLabAPIError for why.
func TestTranslateGitLabErr_SplitsOnStatusWithoutGuessingReasons(t *testing.T) {
	rejected := translateGitLabErr(&gitlab.APIError{
		Status:  http.StatusConflict,
		Message: "Another open merge request already exists",
	})
	if !errors.Is(rejected, ErrProviderRejected) {
		t.Fatalf("4xx err = %v, want a rejection", rejected)
	}
	if errors.Is(rejected, ErrSelfReview) {
		t.Error("the GitLab adapter must not infer a self-review reason yet")
	}

	var providerErr *ProviderError
	if !errors.As(rejected, &providerErr) {
		t.Fatalf("err = %v, want *ProviderError", rejected)
	}
	if providerErr.Provider != "gitlab" {
		t.Errorf("Provider = %q, want gitlab", providerErr.Provider)
	}
	if providerErr.Reason != "" {
		t.Errorf("Reason = %q, want empty", providerErr.Reason)
	}

	outage := translateGitLabErr(&gitlab.APIError{Status: http.StatusInternalServerError, Message: "boom"})
	if errors.Is(outage, ErrProviderRejected) {
		t.Error("a 5xx must not be reported as a rejection")
	}
}

func TestProviderError_IsRejectsUnrelatedTargets(t *testing.T) {
	err := &ProviderError{Provider: "github", Status: http.StatusConflict, Reason: ReasonSelfReview}

	if !errors.Is(err, ErrSelfReview) || !errors.Is(err, ErrProviderRejected) {
		t.Fatal("a self-review rejection must match both sentinels")
	}
	for _, unrelated := range []error{ErrNotFound, ErrUnauthorized, ErrRateLimited, ErrUnsupportedCapability} {
		if errors.Is(err, unrelated) {
			t.Errorf("ProviderError must not match %v", unrelated)
		}
	}
}
