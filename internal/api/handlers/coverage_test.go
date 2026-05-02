package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// Minimal mock that satisfies just the storage.Repository methods exercised
// by CoverageService for these handler tests. Other methods come from the
// embedded nil — calling them would panic, but the tests don't.
type fakeStore struct {
	storage.Repository
	tokens     map[string]*models.CoverageUploadToken // by hash
	tokensByID map[string]*models.CoverageUploadToken
	uploads    []*models.CoverageUpload
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tokens:     map[string]*models.CoverageUploadToken{},
		tokensByID: map[string]*models.CoverageUploadToken{},
	}
}

func (f *fakeStore) CreateCoverageUpload(ctx context.Context, u *models.CoverageUpload) error {
	if u.ID == "" {
		u.ID = "u-" + u.CommitSHA
	}
	u.CreatedAt = time.Now().UTC()
	f.uploads = append(f.uploads, u)
	return nil
}
func (f *fakeStore) GetLatestCoverageUpload(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error) {
	for i := len(f.uploads) - 1; i >= 0; i-- {
		if f.uploads[i].RepositoryID == repoID && f.uploads[i].CommitSHA == sha {
			return f.uploads[i], nil
		}
	}
	return nil, nil
}
func (f *fakeStore) PatchCodeAnalysisCoverage(ctx context.Context, repoID, sha string, c, t int, p float64, s string) (int64, error) {
	return 0, nil
}
func (f *fakeStore) CreateCoverageUploadToken(ctx context.Context, tok *models.CoverageUploadToken) error {
	if tok.ID == "" {
		tok.ID = "tk-" + tok.Name
	}
	tok.CreatedAt = time.Now().UTC()
	f.tokens[tok.TokenHash] = tok
	f.tokensByID[tok.ID] = tok
	return nil
}
func (f *fakeStore) GetCoverageUploadTokenByHash(ctx context.Context, hash string) (*models.CoverageUploadToken, error) {
	return f.tokens[hash], nil
}
func (f *fakeStore) GetCoverageUploadToken(ctx context.Context, id string) (*models.CoverageUploadToken, error) {
	return f.tokensByID[id], nil
}
func (f *fakeStore) ListCoverageUploadTokens(ctx context.Context, repoID string) ([]*models.CoverageUploadToken, error) {
	out := []*models.CoverageUploadToken{}
	for _, t := range f.tokensByID {
		if t.RepositoryID == repoID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *fakeStore) RevokeCoverageUploadToken(ctx context.Context, id string) error {
	t, ok := f.tokensByID[id]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	t.RevokedAt = &now
	return nil
}
func (f *fakeStore) TouchCoverageUploadTokenUsage(ctx context.Context, id string) error {
	if t, ok := f.tokensByID[id]; ok {
		now := time.Now().UTC()
		t.LastUsedAt = &now
	}
	return nil
}
func (f *fakeStore) ListCoverageUploadsForCommit(ctx context.Context, repoID, sha string) ([]*models.CoverageUpload, error) {
	return nil, nil
}

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter(store *fakeStore) (*gin.Engine, *services.CoverageService) {
	svc := services.NewCoverageService(store)
	h := NewCoverageHandler(svc)
	r := gin.New()
	r.POST("/repositories/:id/coverage", h.IngestCoverage)
	r.POST("/repositories/:id/coverage/tokens", func(c *gin.Context) {
		c.Set("user_id", "user-1")
		h.CreateCoverageToken(c)
	})
	r.GET("/repositories/:id/coverage/tokens", h.ListCoverageTokens)
	r.DELETE("/repositories/:id/coverage/tokens/:tokenID", h.RevokeCoverageToken)
	return r, svc
}

const sampleGo = `mode: set
github.com/x/y/a.go:1.1,2.10 1 1
github.com/x/y/a.go:3.1,4.10 1 0
`

func mintToken(t *testing.T, svc *services.CoverageService, repoID string) string {
	t.Helper()
	plain, _, err := svc.CreateUploadToken(context.Background(), repoID, "ci", "user-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func TestCoverageHandler_Ingest_HappyPath(t *testing.T) {
	store := newFakeStore()
	r, svc := newTestRouter(store)
	tok := mintToken(t, svc, "repo-1")

	req := httptest.NewRequest("POST", "/repositories/repo-1/coverage", bytes.NewBufferString(sampleGo))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Coverage-Format", "go")
	req.Header.Set("X-Commit-SHA", "abcdef1234567890")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp models.CoverageUploadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.LinesCovered != 1 || resp.LinesTotal != 2 || resp.Status != "ok" {
		t.Fatalf("body = %+v, want covered=1 total=2 ok", resp)
	}
}

func TestCoverageHandler_Ingest_MissingHeaders(t *testing.T) {
	store := newFakeStore()
	r, svc := newTestRouter(store)
	tok := mintToken(t, svc, "repo-1")

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"missing Authorization", map[string]string{"X-Coverage-Format": "go", "X-Commit-SHA": "abcdef0"}, http.StatusUnauthorized},
		{"missing format", map[string]string{"Authorization": "Bearer " + tok, "X-Commit-SHA": "abcdef0"}, http.StatusBadRequest},
		{"missing sha", map[string]string{"Authorization": "Bearer " + tok, "X-Coverage-Format": "go"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/repositories/repo-1/coverage", strings.NewReader(sampleGo))
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestCoverageHandler_Ingest_BadToken(t *testing.T) {
	store := newFakeStore()
	r, _ := newTestRouter(store)

	req := httptest.NewRequest("POST", "/repositories/repo-1/coverage", strings.NewReader(sampleGo))
	req.Header.Set("Authorization", "Bearer cov_unknown")
	req.Header.Set("X-Coverage-Format", "go")
	req.Header.Set("X-Commit-SHA", "abcdef0")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestCoverageHandler_Ingest_UnsupportedFormat(t *testing.T) {
	store := newFakeStore()
	r, svc := newTestRouter(store)
	tok := mintToken(t, svc, "repo-1")

	req := httptest.NewRequest("POST", "/repositories/repo-1/coverage", strings.NewReader("ignored"))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Coverage-Format", "clover")
	req.Header.Set("X-Commit-SHA", "abcdef0")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", w.Code)
	}
}

func TestCoverageHandler_Ingest_MalformedBody(t *testing.T) {
	store := newFakeStore()
	r, svc := newTestRouter(store)
	tok := mintToken(t, svc, "repo-1")

	req := httptest.NewRequest("POST", "/repositories/repo-1/coverage", strings.NewReader("not a coverage profile"))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Coverage-Format", "go")
	req.Header.Set("X-Commit-SHA", "abcdef0")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
}

func TestCoverageHandler_TokenLifecycle(t *testing.T) {
	store := newFakeStore()
	r, _ := newTestRouter(store)

	// Create
	body := bytes.NewBufferString(`{"name":"github-actions"}`)
	req := httptest.NewRequest("POST", "/repositories/repo-1/coverage/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	var created models.CreateCoverageTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "cov_") {
		t.Fatalf("token prefix wrong: %q", created.Token)
	}

	// List
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/repositories/repo-1/coverage/tokens", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var list []models.CoverageTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	// Revoke
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/repositories/repo-1/coverage/tokens/"+created.ID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, body=%s", w.Code, w.Body.String())
	}

	// Revoke missing
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/repositories/repo-1/coverage/tokens/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("revoke missing status = %d", w.Code)
	}
}
