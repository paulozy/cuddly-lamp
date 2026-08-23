package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── GetFileContent ───────────────────────────────────────────────────────────

func TestClient_GetFileContentReturnsRawBytes(t *testing.T) {
	const body = "module github.com/org/shared\n\ngo 1.25\n"

	var gotAccept, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetFileContent(context.Background(), "org", "shared", "main", "go.mod", 1<<20)
	if err != nil {
		t.Fatalf("GetFileContent() error = %v, want nil", err)
	}
	if string(got) != body {
		t.Errorf("content = %q, want %q", got, body)
	}
	// Raw mode is what makes this one request with no base64 envelope to decode.
	if gotAccept != "application/vnd.github.raw+json" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/vnd.github.raw+json")
	}
	if gotPath != "/repos/org/shared/contents/go.mod" {
		t.Errorf("path = %q, want %q", gotPath, "/repos/org/shared/contents/go.mod")
	}
	if gotQuery != "ref=main" {
		t.Errorf("query = %q, want %q", gotQuery, "ref=main")
	}
}

// A nested path has to survive as a path: the contents endpoint addresses the
// file by directory structure, so escaping the separators would 404.
func TestClient_GetFileContentKeepsPathSeparators(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("stages:\n"))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).GetFileContent(context.Background(), "org", "shared", "main",
		".gitlab/ci/build.gitlab-ci.yml", 1<<20); err != nil {
		t.Fatalf("GetFileContent() error = %v, want nil", err)
	}
	want := "/repos/org/shared/contents/.gitlab/ci/build.gitlab-ci.yml"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

// The three failure classes have to stay distinct: only ErrNotFound lets a
// deriver keep its fact complete, and collapsing a 429 into it is what would
// let a rate limit look like a deleted dependency.
func TestClient_GetFileContentDistinguishesFailureClasses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		want    error
	}{
		{name: "absent", status: http.StatusNotFound, want: ErrNotFound},
		{name: "bad token", status: http.StatusUnauthorized, want: ErrUnauthorized},
		{
			name:    "throttled",
			status:  http.StatusForbidden,
			headers: map[string]string{"x-ratelimit-remaining": "0"},
			want:    ErrRateLimited,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			_, err := newTestClient(srv).GetFileContent(context.Background(), "org", "shared", "main", "go.mod", 1<<20)
			if !errors.Is(err, tt.want) {
				t.Errorf("GetFileContent() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// A provider that reports no size leaves the byte cap as the only guard, so it
// has to actually truncate rather than buffer the whole response.
func TestClient_GetFileContentTruncatesAtLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetFileContent(context.Background(), "org", "shared", "main", "big.json", 64)
	if err != nil {
		t.Fatalf("GetFileContent() error = %v, want nil", err)
	}
	if len(got) != 64 {
		t.Errorf("len(content) = %d, want 64", len(got))
	}
}
