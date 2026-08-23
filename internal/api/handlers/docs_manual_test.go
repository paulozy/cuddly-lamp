package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

// manualDocsRepo serves the repository lookup on top of the shared org double.
type manualDocsRepo struct {
	orgDocsRepo
	repo *models.Repository
}

func (r *manualDocsRepo) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	if r.repo == nil || r.repo.ID != id {
		return nil, nil
	}
	return r.repo, nil
}

// newManualHandler builds the handler with a **nil** enqueuer.
//
// That is the assertion, not an omission: writing a document by hand must not
// depend on Redis/asynq being up, so any attempt to queue work would panic and
// fail the test. That independence is why the manual action can be the primary
// one in the UI — it is the one that always works.
func newManualHandler(t *testing.T, repo *manualDocsRepo) *DocsHandler {
	t.Helper()
	return NewDocsHandler(repo, nil, scm.Credentials{})
}

func manualRepo() *manualDocsRepo {
	return &manualDocsRepo{
		repo: &models.Repository{
			ID:             "repo-1",
			OrganizationID: "org-1",
			URL:            "https://github.com/owner/repo",
		},
	}
}

func postManual(h func(*gin.Context), role models.UserRole, path, body string, params gin.Params) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = params
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = withClaims(req, role)
	h(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func repoParams() gin.Params { return gin.Params{{Key: "id", Value: "repo-1"}} }

// The gap this closes: there was no way to write documentation by hand, so the
// AI path was the only path.
func TestCreateRepositoryDoc_StoresAHandWrittenDocument(t *testing.T) {
	repo := manualRepo()
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateRepositoryDoc, models.RoleDeveloper, "/repositories/repo-1/docs",
		`{"type":"architecture","content":"# Arquitetura\n\nEscrito à mão."}`, repoParams())

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
	if len(repo.createdDocs) != 1 {
		t.Fatalf("created %d documents, want 1", len(repo.createdDocs))
	}

	doc := repo.createdDocs[0]
	if doc.Source != models.DocSourceManual {
		t.Errorf("source = %q, want manual", doc.Source)
	}
	// Completed on arrival: there is no job, so any other status would leave
	// the client polling for an event that never comes.
	if doc.Status != models.DocGenerationStatusCompleted {
		t.Errorf("status = %q, want completed", doc.Status)
	}
	if doc.TokensUsed != 0 {
		t.Errorf("tokens_used = %d, want 0 — nothing was generated", doc.TokensUsed)
	}
	if doc.Scope != models.DocGenerationScopeRepo || doc.RepositoryID == nil || *doc.RepositoryID != "repo-1" {
		t.Errorf("scope/repository = %q/%v, want repo/repo-1", doc.Scope, doc.RepositoryID)
	}
	if got := doc.Content.Data()["architecture"]; !strings.Contains(got, "Escrito à mão") {
		t.Errorf("content = %q, want the submitted markdown", got)
	}
}

// The whole point of the manual path: none of the generation path's four
// dependencies apply. No Anthropic key is configured on this org.
func TestCreateRepositoryDoc_WorksWithoutAnAnthropicKey(t *testing.T) {
	repo := manualRepo()
	repo.orgConfig = &models.OrganizationConfig{} // no AnthropicAPIKey
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateRepositoryDoc, models.RoleDeveloper, "/repositories/repo-1/docs",
		`{"type":"guidelines","content":"# Diretrizes"}`, repoParams())

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 without an Anthropic key (body: %s)", rec.Code, rec.Body)
	}
}

// The token budget governs Claude calls. A hand-written document spends none,
// so an exhausted budget must not block it.
func TestCreateRepositoryDoc_IgnoresTheTokenBudget(t *testing.T) {
	repo := manualRepo()
	repo.orgConfig = &models.OrganizationConfig{AnthropicAPIKey: "sk-test", AnthropicTokensPerHour: 10}
	repo.tokensUsed = 999999
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateRepositoryDoc, models.RoleDeveloper, "/repositories/repo-1/docs",
		`{"type":"adr","content":"# ADR"}`, repoParams())

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 with the budget exhausted (body: %s)", rec.Code, rec.Body)
	}
}

func TestCreateRepositoryDoc_RejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown type", body: `{"type":"runbook","content":"# x"}`},
		{name: "empty type", body: `{"type":"","content":"# x"}`},
		{name: "blank content", body: `{"type":"adr","content":"   "}`},
		{name: "malformed json", body: `{"type":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := manualRepo()
			h := newManualHandler(t, repo)

			rec := postManual(h.CreateRepositoryDoc, models.RoleDeveloper, "/repositories/repo-1/docs", tt.body, repoParams())

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
			}
			if len(repo.createdDocs) != 0 {
				t.Error("a document was stored despite the request being rejected")
			}
		})
	}
}

func TestCreateRepositoryDoc_RefusesAnotherOrganizationsRepository(t *testing.T) {
	repo := manualRepo()
	repo.repo.OrganizationID = "org-2"
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateRepositoryDoc, models.RoleDeveloper, "/repositories/repo-1/docs",
		`{"type":"adr","content":"# ADR"}`, repoParams())

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body)
	}
	if len(repo.createdDocs) != 0 {
		t.Error("a document was stored for a repository outside the caller's organization")
	}
}

func TestCreateRepositoryDoc_MissingRepository(t *testing.T) {
	repo := &manualDocsRepo{}
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateRepositoryDoc, models.RoleDeveloper, "/repositories/repo-1/docs",
		`{"type":"adr","content":"# ADR"}`, repoParams())

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rec.Code, rec.Body)
	}
}

// Org-wide documentation speaks for the whole organization, so writing one by
// hand is gated the same way as generating one.
func TestCreateOrgDoc_RequiresAdmin(t *testing.T) {
	repo := &manualDocsRepo{}
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateOrgDoc, models.RoleDeveloper, "/organizations/docs",
		`{"type":"guidelines","content":"# Diretrizes"}`, nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body)
	}
	if len(repo.createdDocs) != 0 {
		t.Error("a non-admin managed to store an organization document")
	}
}

func TestCreateOrgDoc_StoresWithoutARepository(t *testing.T) {
	repo := &manualDocsRepo{}
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateOrgDoc, models.RoleAdmin, "/organizations/docs",
		`{"type":"architecture","content":"# Visão geral"}`, nil)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body)
	}
	doc := repo.createdDocs[0]
	if doc.Scope != models.DocGenerationScopeOrg {
		t.Errorf("scope = %q, want org", doc.Scope)
	}
	if doc.RepositoryID != nil {
		t.Errorf("repository_id = %v, want nil for an org document", doc.RepositoryID)
	}
	if doc.Source != models.DocSourceManual {
		t.Errorf("source = %q, want manual", doc.Source)
	}
}

// `service_doc` describes one service, so it has no org-wide meaning. The
// manual path reuses the generation path's per-scope vocabulary rather than
// keeping a second list that could drift.
func TestCreateOrgDoc_RejectsARepoOnlyType(t *testing.T) {
	repo := &manualDocsRepo{}
	h := newManualHandler(t, repo)

	rec := postManual(h.CreateOrgDoc, models.RoleAdmin, "/organizations/docs",
		`{"type":"service_doc","content":"# Serviço"}`, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
	}
}

// The response has to carry provenance, or a client cannot tell a document it
// wrote from one Claude produced.
func TestCreateRepositoryDoc_ResponseCarriesProvenance(t *testing.T) {
	h := newManualHandler(t, manualRepo())

	rec := postManual(h.CreateRepositoryDoc, models.RoleDeveloper, "/repositories/repo-1/docs",
		`{"type":"adr","content":"# ADR"}`, repoParams())

	var got models.DocGenerationSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Source != models.DocSourceManual {
		t.Errorf("response source = %q, want manual", got.Source)
	}
	if got.Status != models.DocGenerationStatusCompleted {
		t.Errorf("response status = %q, want completed", got.Status)
	}
}
