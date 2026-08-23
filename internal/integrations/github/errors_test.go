package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_ClassifiesFailureStatuses pins the mapping the rest of the stack
// depends on. The 403 rows are the ones worth reading twice: the same status
// means two different things, and every 403 used to be reported as throttling.
func TestClient_ClassifiesFailureStatuses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		headers     map[string]string
		body        string
		wantErr     error
		wantAPIErr  bool
		wantStatus  int
		wantMessage string
	}{
		{
			name:    "401 is a bad token",
			status:  http.StatusUnauthorized,
			wantErr: ErrUnauthorized,
		},
		{
			name:    "404 is a missing resource",
			status:  http.StatusNotFound,
			wantErr: ErrNotFound,
		},
		{
			name:    "403 with an exhausted quota is throttling",
			status:  http.StatusForbidden,
			headers: map[string]string{"X-RateLimit-Remaining": "0"},
			wantErr: ErrRateLimited,
		},
		{
			name:    "403 with a backoff instruction is throttling",
			status:  http.StatusForbidden,
			headers: map[string]string{"Retry-After": "60"},
			wantErr: ErrRateLimited,
		},
		{
			name:        "403 without either is a permission problem, not throttling",
			status:      http.StatusForbidden,
			headers:     map[string]string{"X-RateLimit-Remaining": "4999"},
			body:        `{"message":"Resource not accessible by integration"}`,
			wantAPIErr:  true,
			wantStatus:  http.StatusForbidden,
			wantMessage: "Resource not accessible by integration",
		},
		{
			name:        "422 keeps its status and detail",
			status:      http.StatusUnprocessableEntity,
			body:        `{"message":"Unprocessable Entity","errors":["Review Can not approve your own pull request"]}`,
			wantAPIErr:  true,
			wantStatus:  http.StatusUnprocessableEntity,
			wantMessage: "Unprocessable Entity",
		},
		{
			name:       "409 keeps its status",
			status:     http.StatusConflict,
			body:       `{"message":"Conflict"}`,
			wantAPIErr: true,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "500 keeps its status so callers can tell it apart",
			status:     http.StatusInternalServerError,
			body:       `{"message":"Server Error"}`,
			wantAPIErr: true,
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tt.status)
				if tt.body != "" {
					w.Write([]byte(tt.body))
				}
			}))
			defer srv.Close()

			_, err := newTestClient(srv).GetRepository(context.Background(), "owner", "repo")

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v (%T), want *APIError", err, err)
			}
			if apiErr.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", apiErr.Status, tt.wantStatus)
			}
			if tt.wantMessage != "" && apiErr.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", apiErr.Message, tt.wantMessage)
			}
		})
	}
}

// TestNewAPIError_ReadsBothErrorsShapes covers the reason GitHub's `errors`
// field is parsed leniently: it is a list of strings on some endpoints and a
// list of objects on others, and losing the detail loses the only sentence
// worth showing a person.
func TestNewAPIError_ReadsBothErrorsShapes(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMessage string
		wantErrors  []string
	}{
		{
			name:        "errors as strings",
			body:        `{"message":"Unprocessable Entity","errors":["Review Can not approve your own pull request"]}`,
			wantMessage: "Unprocessable Entity",
			wantErrors:  []string{"Review Can not approve your own pull request"},
		},
		{
			name:        "errors as objects uses the message",
			body:        `{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"head","message":"No commits between main and feature"}]}`,
			wantMessage: "Validation Failed",
			wantErrors:  []string{"No commits between main and feature"},
		},
		{
			name:        "errors as objects falls back to the code",
			body:        `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom"}]}`,
			wantMessage: "Validation Failed",
			wantErrors:  []string{"custom"},
		},
		{
			name:        "unparseable body keeps the raw text",
			body:        `<html>gateway exploded</html>`,
			wantMessage: "<html>gateway exploded</html>",
		},
		{
			name:        "empty body falls back to the status text",
			body:        ``,
			wantMessage: http.StatusText(http.StatusUnprocessableEntity),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := newAPIError(http.StatusUnprocessableEntity, []byte(tt.body))

			if apiErr.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", apiErr.Message, tt.wantMessage)
			}
			if len(apiErr.Errors) != len(tt.wantErrors) {
				t.Fatalf("Errors = %v, want %v", apiErr.Errors, tt.wantErrors)
			}
			for i, want := range tt.wantErrors {
				if apiErr.Errors[i] != want {
					t.Errorf("Errors[%d] = %q, want %q", i, apiErr.Errors[i], want)
				}
			}
		})
	}
}

// TestNewAPIError_TruncatesUnboundedBody guards the read limit. GitHub does not
// cap error payload size and this text can reach a log line or an API response.
func TestClient_TruncatesUnboundedErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(strings.Repeat("x", maxErrorBodyBytes*3)))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetRepository(context.Background(), "owner", "repo")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if len(apiErr.Message) > maxErrorBodyBytes {
		t.Errorf("Message is %d bytes, want at most %d", len(apiErr.Message), maxErrorBodyBytes)
	}
}

func TestAPIError_ErrorIncludesDetail(t *testing.T) {
	apiErr := &APIError{
		Status:  http.StatusUnprocessableEntity,
		Message: "Unprocessable Entity",
		Errors:  []string{"Review Can not approve your own pull request"},
	}

	got := apiErr.Error()
	if !strings.Contains(got, "422") {
		t.Errorf("Error() = %q, want it to carry the status", got)
	}
	if !strings.Contains(got, "approve your own") {
		t.Errorf("Error() = %q, want it to carry the actionable detail", got)
	}
}

func TestClient_GetAuthenticatedUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"login":"paulozy","name":"Paulo Abreu","type":"User"}`))
	}))
	defer srv.Close()

	user, err := newTestClient(srv).GetAuthenticatedUser(context.Background())
	if err != nil {
		t.Fatalf("GetAuthenticatedUser: %v", err)
	}
	if user.Login != "paulozy" || user.Type != "User" {
		t.Errorf("user = %+v, want paulozy / User", user)
	}
}
