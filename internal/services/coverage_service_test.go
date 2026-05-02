package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/coverage"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

type mockCoverageRepo struct {
	storage.Repository
	tokens     map[string]*models.CoverageUploadToken // by hash
	tokensByID map[string]*models.CoverageUploadToken
	uploads    []*models.CoverageUpload
	patches    []patchCall
	patchAffected int64
	patchErr   error
	createTokErr error
	createUpErr  error
}

type patchCall struct {
	repoID, sha, status string
	covered, total      int
	pct                 float64
}

func newMockCoverageRepo() *mockCoverageRepo {
	return &mockCoverageRepo{
		tokens:     make(map[string]*models.CoverageUploadToken),
		tokensByID: make(map[string]*models.CoverageUploadToken),
	}
}

func (m *mockCoverageRepo) CreateCoverageUpload(ctx context.Context, u *models.CoverageUpload) error {
	if m.createUpErr != nil {
		return m.createUpErr
	}
	if u.ID == "" {
		u.ID = "upload-" + u.CommitSHA
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	m.uploads = append(m.uploads, u)
	return nil
}

func (m *mockCoverageRepo) GetLatestCoverageUpload(ctx context.Context, repoID, sha string) (*models.CoverageUpload, error) {
	var latest *models.CoverageUpload
	for _, u := range m.uploads {
		if u.RepositoryID == repoID && u.CommitSHA == sha {
			if latest == nil || u.CreatedAt.After(latest.CreatedAt) {
				latest = u
			}
		}
	}
	return latest, nil
}

func (m *mockCoverageRepo) PatchCodeAnalysisCoverage(ctx context.Context, repoID, sha string, covered, total int, pct float64, status string) (int64, error) {
	if m.patchErr != nil {
		return 0, m.patchErr
	}
	m.patches = append(m.patches, patchCall{repoID, sha, status, covered, total, pct})
	return m.patchAffected, nil
}

func (m *mockCoverageRepo) CreateCoverageUploadToken(ctx context.Context, t *models.CoverageUploadToken) error {
	if m.createTokErr != nil {
		return m.createTokErr
	}
	if t.ID == "" {
		t.ID = "tok-" + t.Name
	}
	t.CreatedAt = time.Now().UTC()
	m.tokens[t.TokenHash] = t
	m.tokensByID[t.ID] = t
	return nil
}

func (m *mockCoverageRepo) GetCoverageUploadTokenByHash(ctx context.Context, hash string) (*models.CoverageUploadToken, error) {
	return m.tokens[hash], nil
}

func (m *mockCoverageRepo) GetCoverageUploadToken(ctx context.Context, id string) (*models.CoverageUploadToken, error) {
	return m.tokensByID[id], nil
}

func (m *mockCoverageRepo) ListCoverageUploadTokens(ctx context.Context, repoID string) ([]*models.CoverageUploadToken, error) {
	out := make([]*models.CoverageUploadToken, 0)
	for _, t := range m.tokensByID {
		if t.RepositoryID == repoID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *mockCoverageRepo) RevokeCoverageUploadToken(ctx context.Context, id string) error {
	t, ok := m.tokensByID[id]
	if !ok || t.RevokedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	t.RevokedAt = &now
	return nil
}

func (m *mockCoverageRepo) TouchCoverageUploadTokenUsage(ctx context.Context, id string) error {
	t, ok := m.tokensByID[id]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	t.LastUsedAt = &now
	return nil
}

func (m *mockCoverageRepo) ListCoverageUploadsForCommit(ctx context.Context, repoID, sha string) ([]*models.CoverageUpload, error) {
	out := []*models.CoverageUpload{}
	for _, u := range m.uploads {
		if u.RepositoryID == repoID && u.CommitSHA == sha {
			out = append(out, u)
		}
	}
	return out, nil
}

const sampleGoCover = `mode: set
github.com/x/y/a.go:1.1,2.10 1 1
github.com/x/y/a.go:3.1,4.10 1 0
`

func TestCoverageService_IngestCoverage_HappyPath(t *testing.T) {
	repo := newMockCoverageRepo()
	svc := NewCoverageService(repo)
	ctx := context.Background()

	plain, _, err := svc.CreateUploadToken(ctx, "repo-1", "ci", "user-1", nil)
	if err != nil {
		t.Fatalf("CreateUploadToken: %v", err)
	}

	upload, err := svc.IngestCoverage(ctx, IngestRequest{
		RepositoryID: "repo-1",
		Token:        plain,
		Format:       "go",
		CommitSHA:    "abcdef1234567890",
		Branch:       "main",
		Body:         strings.NewReader(sampleGoCover),
	})
	if err != nil {
		t.Fatalf("IngestCoverage: %v", err)
	}
	if upload.LinesCovered != 1 || upload.LinesTotal != 2 {
		t.Fatalf("counts: covered=%d total=%d", upload.LinesCovered, upload.LinesTotal)
	}
	if upload.Status != coverage.StatusOK {
		t.Fatalf("status = %q, want ok", upload.Status)
	}
	if len(repo.patches) != 1 {
		t.Fatalf("expected 1 patch attempt, got %d", len(repo.patches))
	}
	p := repo.patches[0]
	if p.repoID != "repo-1" || p.sha != "abcdef1234567890" || p.covered != 1 || p.total != 2 {
		t.Fatalf("patch wrong: %+v", p)
	}
}

func TestCoverageService_IngestCoverage_RejectsForeignToken(t *testing.T) {
	repo := newMockCoverageRepo()
	svc := NewCoverageService(repo)
	ctx := context.Background()

	plain, _, err := svc.CreateUploadToken(ctx, "repo-A", "ci", "user-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.IngestCoverage(ctx, IngestRequest{
		RepositoryID: "repo-B", // wrong repo
		Token:        plain,
		Format:       "go",
		CommitSHA:    "deadbeef",
		Body:         strings.NewReader(sampleGoCover),
	})
	if !errors.Is(err, ErrCoverageTokenForeignRepo) {
		t.Fatalf("err = %v, want ErrCoverageTokenForeignRepo", err)
	}
}

func TestCoverageService_IngestCoverage_RejectsRevokedToken(t *testing.T) {
	repo := newMockCoverageRepo()
	svc := NewCoverageService(repo)
	ctx := context.Background()

	plain, m, _ := svc.CreateUploadToken(ctx, "repo-1", "ci", "user-1", nil)
	if err := svc.RevokeUploadToken(ctx, "repo-1", m.ID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.IngestCoverage(ctx, IngestRequest{
		RepositoryID: "repo-1",
		Token:        plain,
		Format:       "go",
		CommitSHA:    "deadbeef",
		Body:         strings.NewReader(sampleGoCover),
	})
	if !errors.Is(err, ErrCoverageTokenExpired) {
		t.Fatalf("err = %v, want ErrCoverageTokenExpired", err)
	}
}

func TestCoverageService_IngestCoverage_RejectsExpiredToken(t *testing.T) {
	repo := newMockCoverageRepo()
	svc := NewCoverageService(repo)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	plain, _, _ := svc.CreateUploadToken(ctx, "repo-1", "ci", "user-1", &past)
	_, err := svc.IngestCoverage(ctx, IngestRequest{
		RepositoryID: "repo-1",
		Token:        plain,
		Format:       "go",
		CommitSHA:    "deadbeef",
		Body:         strings.NewReader(sampleGoCover),
	})
	if !errors.Is(err, ErrCoverageTokenExpired) {
		t.Fatalf("err = %v, want ErrCoverageTokenExpired", err)
	}
}

func TestCoverageService_IngestCoverage_InvalidFormat(t *testing.T) {
	repo := newMockCoverageRepo()
	svc := NewCoverageService(repo)
	plain, _, _ := svc.CreateUploadToken(context.Background(), "repo-1", "ci", "user-1", nil)

	_, err := svc.IngestCoverage(context.Background(), IngestRequest{
		RepositoryID: "repo-1",
		Token:        plain,
		Format:       "clover",
		CommitSHA:    "abcdef0",
		Body:         strings.NewReader("ignored"),
	})
	if !errors.Is(err, ErrCoverageInvalidFormat) {
		t.Fatalf("err = %v, want ErrCoverageInvalidFormat", err)
	}
}

func TestCoverageService_IngestCoverage_InvalidSHA(t *testing.T) {
	repo := newMockCoverageRepo()
	svc := NewCoverageService(repo)
	plain, _, _ := svc.CreateUploadToken(context.Background(), "repo-1", "ci", "user-1", nil)

	_, err := svc.IngestCoverage(context.Background(), IngestRequest{
		RepositoryID: "repo-1",
		Token:        plain,
		Format:       "go",
		CommitSHA:    "not-a-sha",
		Body:         strings.NewReader(sampleGoCover),
	})
	if !errors.Is(err, ErrCoverageInvalidSHA) {
		t.Fatalf("err = %v, want ErrCoverageInvalidSHA", err)
	}
}

func TestCoverageService_IngestCoverage_LastWinsAcrossUploads(t *testing.T) {
	repo := newMockCoverageRepo()
	svc := NewCoverageService(repo)
	ctx := context.Background()
	plain, _, _ := svc.CreateUploadToken(ctx, "repo-1", "ci", "user-1", nil)

	const sha = "abcdef0"
	first := strings.NewReader(`mode: set
github.com/x/a.go:1.1,2.10 4 0
`) // 0/4
	second := strings.NewReader(`mode: set
github.com/x/a.go:1.1,2.10 4 1
`) // 4/4

	if _, err := svc.IngestCoverage(ctx, IngestRequest{RepositoryID: "repo-1", Token: plain, Format: "go", CommitSHA: sha, Body: first}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IngestCoverage(ctx, IngestRequest{RepositoryID: "repo-1", Token: plain, Format: "go", CommitSHA: sha, Body: second}); err != nil {
		t.Fatal(err)
	}

	latest, _ := svc.LookupForAnalysis(ctx, "repo-1", sha)
	if latest == nil || latest.LinesCovered != 4 || latest.LinesTotal != 4 {
		t.Fatalf("latest = %+v, want covered=4 total=4", latest)
	}

	// Two patches were attempted (one per upload), most recent reflects last upload.
	if len(repo.patches) != 2 {
		t.Fatalf("patches = %d, want 2", len(repo.patches))
	}
	if last := repo.patches[1]; last.covered != 4 || last.total != 4 {
		t.Fatalf("last patch wrong: %+v", last)
	}
}

func TestCoverageService_TokenHashingIsDeterministic(t *testing.T) {
	a := hashCoverageToken("cov_abc")
	b := hashCoverageToken("cov_abc")
	c := hashCoverageToken("cov_xyz")
	if a != b {
		t.Fatal("hash should be deterministic")
	}
	if a == c {
		t.Fatal("different inputs must produce different hashes")
	}
	if len(a) != 64 {
		t.Fatalf("hex length = %d, want 64", len(a))
	}
}
