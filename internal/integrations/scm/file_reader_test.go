package scm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/gitlab"
)

// ── FileReader contract ──────────────────────────────────────────────────────
//
// The two hosts address a file completely differently — GitHub by path under
// `contents/`, GitLab by a single segment with the slashes percent-encoded — and
// answer with a different body shape unless asked for raw. A derived edge must
// not depend on which forge the repository lives on, so the same path has to
// come back as the same bytes through both adapters. This is the test that says
// so; without it the whole architecture domain silently works on one host only.

const contractFile = "internal/config/app.yaml"
const contractBody = "database:\n  host: db.prod.internal\n"

func githubRawServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/service/contents/"+contractFile {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(contractBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func gitlabRawServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath keeps %2F intact, which is the whole point: a client that
		// sent raw slashes would land on a different, longer path and 404.
		want := "/projects/org%2Fservice/repository/files/" +
			strings.ReplaceAll(contractFile, "/", "%2F") + "/raw"
		if r.URL.EscapedPath() != want {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(contractBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFileReader_SamePathYieldsSameBytesOnBothProviders(t *testing.T) {
	ref := RepoRef{Namespace: "org", Name: "service"}
	providers := map[string]Provider{
		"github": &githubProvider{client: github.NewClientWithBaseURL("t", githubRawServer(t).URL), token: "t"},
		"gitlab": &gitlabProvider{client: gitlab.NewClientWithBaseURL("t", gitlabRawServer(t).URL), token: "t"},
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			got, err := provider.GetFileContent(context.Background(), ref, "main", contractFile)
			if err != nil {
				t.Fatalf("GetFileContent() error = %v, want nil", err)
			}
			if string(got) != contractBody {
				t.Errorf("content = %q, want %q", got, contractBody)
			}
		})
	}
}

func TestFileReader_AbsentFileIsErrNotFoundOnBothProviders(t *testing.T) {
	ref := RepoRef{Namespace: "org", Name: "service"}
	providers := map[string]Provider{
		"github": &githubProvider{client: github.NewClientWithBaseURL("t", githubRawServer(t).URL), token: "t"},
		"gitlab": &gitlabProvider{client: gitlab.NewClientWithBaseURL("t", gitlabRawServer(t).URL), token: "t"},
	}

	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			_, err := provider.GetFileContent(context.Background(), ref, "main", "does/not/exist.yaml")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("GetFileContent() error = %v, want ErrNotFound", err)
			}
		})
	}
}
