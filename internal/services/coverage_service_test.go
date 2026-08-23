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
	tokens       map[string]*models.CoverageUploadToken // by hash
	tokensByID   map[string]*models.CoverageUploadToken
	uploads      []*models.CoverageUpload
	createTokErr error
	createUpErr  error
	// repos and orgConfig back BuildSetup. orgConfigErr proves that a failure to
	// read the config falls back to the platform URL rather than refusing to
	// render the panel.
	repos        map[string]*models.Repository
	orgConfig    *models.OrganizationConfig
	orgConfigErr error
}

func (m *mockCoverageRepo) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	return m.repos[id], nil
}

func (m *mockCoverageRepo) GetOrganizationConfig(_ context.Context, _ string) (*models.OrganizationConfig, error) {
	if m.orgConfigErr != nil {
		return nil, m.orgConfigErr
	}
	return m.orgConfig, nil
}

func newMockCoverageRepo() *mockCoverageRepo {
	return &mockCoverageRepo{
		tokens:     make(map[string]*models.CoverageUploadToken),
		tokensByID: make(map[string]*models.CoverageUploadToken),
		repos:      make(map[string]*models.Repository),
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
	if upload.RepositoryID != "repo-1" || upload.CommitSHA != "abcdef1234567890" {
		t.Fatalf("upload keyed wrong: repo=%q sha=%q", upload.RepositoryID, upload.CommitSHA)
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

	// Both uploads are retained; LookupForAnalysis above already asserted that
	// the most recent one wins.
	if len(repo.uploads) != 2 {
		t.Fatalf("uploads = %d, want 2", len(repo.uploads))
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

// ── BuildSetup ───────────────────────────────────────────────────────────────

const setupOrgID = "org-1"

func setupRepo(overrides func(*models.Repository)) *models.Repository {
	repo := &models.Repository{
		ID:             "repo-1",
		OrganizationID: setupOrgID,
		Type:           models.RepositoryTypeGitHub,
		Metadata: models.RepositoryMetadata{
			DefaultBranch: "main",
			Languages:     map[string]int{"Go": 98, "Shell": 2},
		},
	}
	if overrides != nil {
		overrides(repo)
	}
	return repo
}

func buildSetup(t *testing.T, store *mockCoverageRepo, platformBaseURL string) *models.CoverageSetupResponse {
	t.Helper()
	svc := NewCoverageService(store)
	setup, err := svc.BuildSetup(context.Background(), "repo-1", setupOrgID, platformBaseURL)
	if err != nil {
		t.Fatalf("BuildSetup() error = %v, want nil", err)
	}
	return setup
}

func TestBuildSetup_ResolvesTheIngestURLAndTheSingleSecret(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)

	setup := buildSetup(t, store, "https://idp.example.com")

	if !setup.Reachable {
		t.Fatal("Reachable = false, want true for a public https URL")
	}
	// The value the panel used to never show, which is the whole reason this
	// endpoint exists.
	if setup.BaseURL != "https://idp.example.com" {
		t.Errorf("base_url = %q, want the platform URL", setup.BaseURL)
	}
	want := "https://idp.example.com/api/v1/repositories/repo-1/coverage"
	if setup.IngestURL != want {
		t.Errorf("ingest_url = %q, want %q", setup.IngestURL, want)
	}
	// Exactly one of the three inputs is a secret. Treating all three as secrets
	// is what turned a one-step setup into a three-step one nobody finished.
	if setup.SecretEnvName != "IDP_COVERAGE_TOKEN" {
		t.Errorf("secret_env_name = %q, want IDP_COVERAGE_TOKEN", setup.SecretEnvName)
	}
	// Header names come from the endpoint's own constants so a snippet cannot
	// name a header the endpoint does not read.
	if setup.Headers.Format != coverage.HeaderFormat ||
		setup.Headers.Commit != coverage.HeaderCommitSHA ||
		setup.Headers.Branch != coverage.HeaderBranch {
		t.Errorf("headers = %+v, want the ingest endpoint's own names", setup.Headers)
	}
}

// A trailing slash on the configured URL must not produce a double slash in the
// path — the endpoint would 404 and the person would have no idea why.
func TestBuildSetup_TrimsATrailingSlashOnTheBaseURL(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)

	setup := buildSetup(t, store, "https://idp.example.com/")

	want := "https://idp.example.com/api/v1/repositories/repo-1/coverage"
	if setup.IngestURL != want {
		t.Errorf("ingest_url = %q, want %q", setup.IngestURL, want)
	}
}

// The organization's own URL wins over the platform default, mirroring how
// provider credentials already resolve. OrganizationConfig.WebhookBaseURL has been
// persisted since migration 008 and never read; this is its first reader.
func TestBuildSetup_OrganizationOverrideBeatsThePlatformURL(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)
	store.orgConfig = &models.OrganizationConfig{WebhookBaseURL: "https://tenant.example.com"}

	setup := buildSetup(t, store, "https://platform.example.com")

	if setup.BaseURL != "https://tenant.example.com" {
		t.Errorf("base_url = %q, want the organization override", setup.BaseURL)
	}
}

// An unreadable config falls back to the platform URL: refusing to render the
// panel over it would be a worse answer than the one we can still give.
func TestBuildSetup_UnreadableOrgConfigFallsBackToThePlatformURL(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)
	store.orgConfigErr = errors.New("db down")

	setup := buildSetup(t, store, "https://platform.example.com")

	if setup.BaseURL != "https://platform.example.com" {
		t.Errorf("base_url = %q, want the platform fallback", setup.BaseURL)
	}
}

// The most important case. A CI runner cannot reach loopback, so emitting a
// snippet pointing at it would produce a step that fails on every single run —
// which is exactly the silent breakage this endpoint exists to end. No ingest URL
// is offered at all, so the client has nothing to render a snippet from.
func TestBuildSetup_UnreachableBaseURLOffersNoIngestURL(t *testing.T) {
	for _, baseURL := range []string{
		"",
		"   ",
		"http://localhost:3000",
		"http://127.0.0.1:3000",
	} {
		t.Run(baseURL, func(t *testing.T) {
			store := newMockCoverageRepo()
			store.repos["repo-1"] = setupRepo(nil)

			setup := buildSetup(t, store, baseURL)

			if setup.Reachable {
				t.Errorf("Reachable = true for %q, want false", baseURL)
			}
			if setup.IngestURL != "" {
				t.Errorf("ingest_url = %q, want empty so no snippet can be built", setup.IngestURL)
			}
		})
	}
}

func TestBuildSetup_SuggestsFromTheRepositoryLanguages(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)

	setup := buildSetup(t, store, "https://idp.example.com")

	if setup.Suggestion.Format != string(coverage.FormatGo) {
		t.Errorf("suggested format = %q, want go", setup.Suggestion.Format)
	}
	if setup.Suggestion.ReportPath != "coverage.out" {
		t.Errorf("suggested report_path = %q, want coverage.out", setup.Suggestion.ReportPath)
	}
	// The language is echoed so the UI can say *why* it picked this, rather than
	// appearing to know something it does not.
	if setup.Suggestion.Language != "go" {
		t.Errorf("suggested language = %q, want go", setup.Suggestion.Language)
	}
	// Every offered format must be one the endpoint accepts, or the selector can
	// produce a snippet that comes back 415.
	if len(setup.Formats) != len(coverage.Formats()) {
		t.Errorf("formats = %v, want all four the parsers support", setup.Formats)
	}
}

// The CI system is read back off the stored evidence path, so an
// already-synced repository gets a correct answer with no re-sync — which matters,
// because nothing backfills.
func TestBuildSetup_ReadsTheCISystemFromTheStoredEvidence(t *testing.T) {
	tests := []struct {
		evidence     string
		wantSystem   string
		wantConfPath string
	}{
		{evidence: ".github/workflows/ci.yml", wantSystem: "ci.github_actions", wantConfPath: ".github/workflows/ci.yml"},
		{evidence: ".gitlab-ci.yml", wantSystem: "ci.gitlab", wantConfPath: ".gitlab-ci.yml"},
		// Unknown stays empty: the client then offers both shapes rather than
		// guessing which one to show.
		{evidence: "Makefile", wantSystem: "", wantConfPath: "Makefile"},
		{evidence: "", wantSystem: "", wantConfPath: ""},
	}
	for _, tt := range tests {
		t.Run(tt.evidence, func(t *testing.T) {
			store := newMockCoverageRepo()
			store.repos["repo-1"] = setupRepo(func(r *models.Repository) {
				r.Metadata.CIEvidence = tt.evidence
			})

			setup := buildSetup(t, store, "https://idp.example.com")

			if setup.CISystem != tt.wantSystem {
				t.Errorf("ci_system = %q, want %q", setup.CISystem, tt.wantSystem)
			}
			if setup.CIConfigPath != tt.wantConfPath {
				t.Errorf("ci_config_path = %q, want %q", setup.CIConfigPath, tt.wantConfPath)
			}
		})
	}
}

// HasCI is tri-state everywhere else this signal appears, and it has to stay that
// way here: nil means the tree could not be fully inspected, never "no CI".
func TestBuildSetup_HasCIStaysTriState(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)

	if setup := buildSetup(t, store, "https://idp.example.com"); setup.HasCI != nil {
		t.Errorf("has_ci = %v, want nil when detection never ran", setup.HasCI)
	}

	hasCI := true
	store.repos["repo-1"] = setupRepo(func(r *models.Repository) { r.Metadata.HasCI = &hasCI })
	setup := buildSetup(t, store, "https://idp.example.com")
	if setup.HasCI == nil || !*setup.HasCI {
		t.Errorf("has_ci = %v, want true", setup.HasCI)
	}
}

// The feedback loop that did not exist anywhere. A token nobody has used means
// the CI is not wired up, and the dogfooding workflow's own guard skips the upload
// silently — so a botched setup was invisible from both ends.
func TestBuildSetup_ReportsATokenThatWasNeverUsed(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)
	svc := NewCoverageService(store)
	if _, _, err := svc.CreateUploadToken(context.Background(), "repo-1", "ci", "user-1", nil); err != nil {
		t.Fatalf("CreateUploadToken() error = %v", err)
	}

	setup := buildSetup(t, store, "https://idp.example.com")

	if !setup.HasActiveToken {
		t.Error("has_active_token = false, want true")
	}
	if setup.LastUploadAt != nil {
		t.Errorf("last_upload_at = %v, want nil for a token the CI never used", setup.LastUploadAt)
	}
}

func TestBuildSetup_ReportsTheMostRecentUpload(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)
	svc := NewCoverageService(store)
	_, token, err := svc.CreateUploadToken(context.Background(), "repo-1", "ci", "user-1", nil)
	if err != nil {
		t.Fatalf("CreateUploadToken() error = %v", err)
	}
	used := time.Now().UTC().Add(-2 * time.Hour)
	token.LastUsedAt = &used

	setup := buildSetup(t, store, "https://idp.example.com")

	if setup.LastUploadAt == nil || !setup.LastUploadAt.Equal(used) {
		t.Errorf("last_upload_at = %v, want %v", setup.LastUploadAt, used)
	}
}

// A revoked token is not a working setup, so it must not read as one.
func TestBuildSetup_RevokedTokenIsNotAnActiveToken(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(nil)
	svc := NewCoverageService(store)
	_, token, err := svc.CreateUploadToken(context.Background(), "repo-1", "ci", "user-1", nil)
	if err != nil {
		t.Fatalf("CreateUploadToken() error = %v", err)
	}
	revoked := time.Now().UTC()
	token.RevokedAt = &revoked

	setup := buildSetup(t, store, "https://idp.example.com")

	if setup.HasActiveToken {
		t.Error("has_active_token = true, want false for a revoked token")
	}
}

// Both layers of authorization apply here as everywhere else: the role gate is on
// the route, and the service independently checks the repository belongs to the
// caller's organization.
func TestBuildSetup_RefusesAForeignRepositoryAndAMissingOne(t *testing.T) {
	store := newMockCoverageRepo()
	store.repos["repo-1"] = setupRepo(func(r *models.Repository) { r.OrganizationID = "other-org" })
	svc := NewCoverageService(store)

	if _, err := svc.BuildSetup(context.Background(), "repo-1", setupOrgID, "https://idp.example.com"); !errors.Is(err, ErrForbidden) {
		t.Errorf("foreign repository error = %v, want ErrForbidden", err)
	}
	if _, err := svc.BuildSetup(context.Background(), "missing", setupOrgID, "https://idp.example.com"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Errorf("missing repository error = %v, want ErrRepositoryNotFound", err)
	}
}
