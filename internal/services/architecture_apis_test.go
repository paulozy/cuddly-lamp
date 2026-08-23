package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

const openAPISpec = `openapi: 3.0.3
info:
  title: Orders API
  version: 1.4.0
paths:
  /orders:
    get: {}
`

// ── the shortlist ────────────────────────────────────────────────────────────

func extractAPIs(t *testing.T, in ExtractInput) (derive.APIsFact, derive.Outcome) {
	t.Helper()
	payload, outcome := apisExtractor{}.Extract(context.Background(), in)
	fact, ok := payload.(derive.APIsFact)
	if !ok {
		t.Fatalf("payload type = %T, want derive.APIsFact", payload)
	}
	return fact, outcome
}

func apiInput(contents map[string]string, sizes map[string]int, truncated bool) ExtractInput {
	paths := make([]string, 0, len(contents))
	for path := range contents {
		paths = append(paths, path)
	}
	reader := &stubFileReader{contents: contents}
	return ExtractInput{
		RepositoryID: "repo-orders",
		Paths:        paths,
		Sizes:        sizes,
		Truncated:    truncated,
		Fetch: func(ctx context.Context, path string) ([]byte, error) {
			return reader.GetFileContent(ctx, scm.RepoRef{}, "main", path)
		},
	}
}

func TestAPIDiscovery_FindsSpecAndIgnoresNonSpecYAML(t *testing.T) {
	fact, outcome := extractAPIs(t, apiInput(map[string]string{
		"openapi.yaml":             openAPISpec,
		"docker-compose.yml":       "services:\n  api:\n    image: postgres:16\n",
		".github/workflows/ci.yml": "on: push\n",
	}, nil, false))

	if !outcome.Complete {
		t.Errorf("outcome = %+v, want complete", outcome)
	}
	if len(fact.Specs) != 1 || fact.Specs[0].Path != "openapi.yaml" {
		t.Fatalf("specs = %+v, want only openapi.yaml", fact.Specs)
	}
	if fact.Specs[0].Title != "Orders API" {
		t.Errorf("title = %q, want Orders API", fact.Specs[0].Title)
	}
}

// Without the vendored filter a dependency's own bundled openapi.yaml is
// catalogued as an API this repository exposes.
func TestAPIDiscovery_IgnoresVendoredSpecs(t *testing.T) {
	fact, _ := extractAPIs(t, apiInput(map[string]string{
		"openapi.yaml":                       openAPISpec,
		"node_modules/some-sdk/openapi.yaml": openAPISpec,
		"vendor/other/docs/api/spec.yaml":    openAPISpec,
	}, nil, false))

	if len(fact.Specs) != 1 || fact.Specs[0].Path != "openapi.yaml" {
		t.Errorf("specs = %+v, want only the repository's own spec", fact.Specs)
	}
}

// A repository with 200 matching files is a specification repository, not a
// service. Marking the fact incomplete is what keeps the truncation honest: a
// short read that looked complete would sweep every spec past the cut.
func TestAPIDiscovery_TooManyCandidatesMarksIncompleteInsteadOfFetchingAll(t *testing.T) {
	contents := make(map[string]string, maxSpecCandidates+5)
	for i := 0; i < maxSpecCandidates+5; i++ {
		contents["openapi/spec-"+itoa(i)+".yaml"] = openAPISpec
	}
	fact, outcome := extractAPIs(t, apiInput(contents, nil, false))

	if outcome.Complete {
		t.Error("outcome.Complete = true, want false above the candidate ceiling")
	}
	if !hasReason(outcome.Reasons, derive.ReasonTooManyCandidates) {
		t.Errorf("reasons = %v, want too_many_candidates", outcome.Reasons)
	}
	if len(fact.Specs) > maxSpecCandidates {
		t.Errorf("specs = %d, want at most %d", len(fact.Specs), maxSpecCandidates)
	}
	// The count before the cut is recorded, so the log can say what was dropped.
	if fact.CandidateCount != maxSpecCandidates+5 {
		t.Errorf("candidate_count = %d, want %d", fact.CandidateCount, maxSpecCandidates+5)
	}
}

func TestAPIDiscovery_SkipsOversizedSpec(t *testing.T) {
	reader := &stubFileReader{contents: map[string]string{"openapi.yaml": openAPISpec}}
	in := ExtractInput{
		RepositoryID: "repo-orders",
		Paths:        []string{"openapi.yaml", "docs/api/generated.json"},
		Sizes:        map[string]int{"openapi.yaml": 400, "docs/api/generated.json": 300 * 1024},
		Fetch: func(ctx context.Context, path string) ([]byte, error) {
			return reader.GetFileContent(ctx, scm.RepoRef{}, "main", path)
		},
	}
	extractAPIs(t, in)

	for _, requested := range reader.requests {
		if requested == "docs/api/generated.json" {
			t.Fatalf("requested = %v, want the 300 KB entry skipped", reader.requests)
		}
	}
}

// A file that fails the sniff is not an API — a decision, not a failure. The fact
// stays complete, because a repository full of near-miss YAML must still be able
// to retract a spec that was genuinely deleted.
func TestAPIDiscovery_NonSpecCandidateKeepsTheFactComplete(t *testing.T) {
	fact, outcome := extractAPIs(t, apiInput(map[string]string{
		"api.yaml": "just: a config\n",
	}, nil, false))

	if !outcome.Complete {
		t.Errorf("outcome = %+v, want complete — a near miss is a decision, not a failure", outcome)
	}
	if len(fact.Specs) != 0 {
		t.Errorf("specs = %+v, want none", fact.Specs)
	}
}

func TestAPIDiscovery_TruncatedTreeIsIncomplete(t *testing.T) {
	_, outcome := extractAPIs(t, apiInput(map[string]string{"openapi.yaml": openAPISpec}, nil, true))
	if outcome.Complete {
		t.Error("outcome.Complete = true, want false for a truncated listing")
	}
}

// ── identity and reconciliation ──────────────────────────────────────────────

func apisFact(t *testing.T, repositoryID string, complete bool, specs ...derive.Spec) models.RepositoryFact {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"data":    derive.APIsFact{Specs: specs, CandidateCount: len(specs)},
		"outcome": derive.Outcome{Complete: complete},
	})
	if err != nil {
		t.Fatalf("marshal fact payload: %v", err)
	}
	return models.RepositoryFact{
		OrganizationID: testOrgID,
		RepositoryID:   repositoryID,
		FactKind:       models.FactKindAPIs,
		Payload:        encoded,
		Complete:       complete,
	}
}

func spec(path, title, version string) derive.Spec {
	return derive.Spec{
		Path: path, Kind: derive.SpecOpenAPI, Title: title, Version: version,
		RuleID: derive.RuleAPIRootMarker, Confidence: derive.ConfidenceExactName,
	}
}

// steppingService is the shared setup: a stepping clock so consecutive
// reconciliations land in different mark-and-sweep windows.
func steppingService(store *architectureStore) *ArchitectureService {
	svc := NewArchitectureService(store, newFakeEnqueuer())
	svc.extractors = nil
	svc.edgeDerivers = nil
	step := 0
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time {
		step++
		return base.Add(time.Duration(step) * time.Hour)
	}
	return svc
}

func reconcileAPIsWith(t *testing.T, store *architectureStore, svc *ArchitectureService, fact models.RepositoryFact) {
	t.Helper()
	store.facts[factKey(fact.RepositoryID, models.FactKindAPIs)] = &fact
	if err := svc.Reconcile(context.Background(), testOrgID); err != nil {
		t.Fatalf("Reconcile() error = %v, want nil", err)
	}
}

func TestAPIDiscovery_InsertsDiscoveredAPI(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "1.4.0")))

	live := store.liveAPIs()
	if len(live) != 1 {
		t.Fatalf("live apis = %d, want 1", len(live))
	}
	got := live[0]
	if got.SpecPath != "openapi.yaml" || got.Kind != models.APIKindOpenAPI {
		t.Errorf("api = %+v, want the openapi at openapi.yaml", got)
	}
	if got.Title != "Orders API" || got.Version != "1.4.0" {
		t.Errorf("title/version = %q/%q, want Orders API/1.4.0", got.Title, got.Version)
	}
	if !got.IsDerived() {
		t.Error("derivation_key is nil, want the per-repository apidisc key")
	}
	if *got.DerivationKey != "apidisc:v1:repo/repo-orders" {
		t.Errorf("derivation_key = %q, want the repository-scoped key", *got.DerivationKey)
	}
}

// `info.title` is display. Renaming it must not create a second API — which is
// what makes the location the identity rather than the name.
func TestAPIDiscovery_TitleChangeDoesNotCreateNewAPI(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "1.4.0")))
	original := store.liveAPIs()[0].ID
	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Order Management API", "1.4.0")))

	live := store.liveAPIs()
	if len(live) != 1 {
		t.Fatalf("live apis = %d, want 1", len(live))
	}
	if live[0].ID != original {
		t.Errorf("id = %q, want the original %q", live[0].ID, original)
	}
	if live[0].Title != "Order Management API" {
		t.Errorf("title = %q, want the new one", live[0].Title)
	}
}

// If the version were in the key, every bump would create a new API and sweep the
// old one — manufacturing a version history that never happened.
func TestAPIDiscovery_VersionBumpDoesNotCreateNewAPI(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "1.4.0")))
	original := store.liveAPIs()[0].ID
	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "2.0.0")))

	live := store.liveAPIs()
	if len(live) != 1 {
		t.Fatalf("live apis = %d, want 1", len(live))
	}
	if live[0].ID != original {
		t.Errorf("id = %q, want the original %q — a bump is not a new API", live[0].ID, original)
	}
	if live[0].Version != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", live[0].Version)
	}
}

// Moving the file *is* a new API plus a sweep of the old one, and that is the
// correct answer: the catalog says where the contract lives now.
func TestAPIDiscovery_MovedSpecCreatesNewAndSweepsOld(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "1.4.0")))
	original := store.liveAPIs()[0].ID
	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("docs/api/openapi.yaml", "Orders API", "1.4.0")))

	live := store.liveAPIs()
	if len(live) != 1 {
		t.Fatalf("live apis = %d, want 1", len(live))
	}
	if live[0].SpecPath != "docs/api/openapi.yaml" {
		t.Errorf("spec_path = %q, want the new location", live[0].SpecPath)
	}
	if live[0].ID == original {
		t.Error("id is unchanged, want a new row — the location is the identity")
	}
}

// A truncated tree cannot authorise deletion: the spec might be there and simply
// unseen. Same gate as the library edges, scoped per repository.
func TestAPIDiscovery_TruncatedTreeDoesNotSweep(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "1.4.0")))
	sweepsBefore := store.apiSweepCalls

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", false))

	if store.apiSweepCalls != sweepsBefore {
		t.Errorf("api sweep calls = %d, want %d — an incomplete fact must not delete",
			store.apiSweepCalls, sweepsBefore)
	}
	if len(store.liveAPIs()) != 1 {
		t.Errorf("live apis = %d, want the spec to survive", len(store.liveAPIs()))
	}
}

func TestAPIDiscovery_DeletedSpecIsSwept(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "1.4.0")))
	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true))

	if got := len(store.liveAPIs()); got != 0 {
		t.Errorf("live apis = %d, want 0", got)
	}
}

// One repository's incomplete tree must not cost another repository its
// retraction: API discovery reads no cross-repository index, which is exactly why
// its sweep is scoped per repository rather than per organization.
func TestAPIDiscovery_OneRepositoryIncompleteDoesNotBlockAnother(t *testing.T) {
	store := newArchitectureStore()
	svc := steppingService(store)

	store.facts[factKey("repo-orders", models.FactKindAPIs)] = ptrFact(apisFact(t, "repo-orders", true, spec("openapi.yaml", "Orders API", "1.0.0")))
	store.facts[factKey("repo-billing", models.FactKindAPIs)] = ptrFact(apisFact(t, "repo-billing", true, spec("openapi.yaml", "Billing API", "1.0.0")))
	if err := svc.Reconcile(context.Background(), testOrgID); err != nil {
		t.Fatalf("Reconcile() error = %v, want nil", err)
	}
	if got := len(store.liveAPIs()); got != 2 {
		t.Fatalf("live apis = %d, want 2", got)
	}

	// billing's spec is gone, and orders can no longer be fully inspected.
	store.facts[factKey("repo-orders", models.FactKindAPIs)] = ptrFact(apisFact(t, "repo-orders", false))
	store.facts[factKey("repo-billing", models.FactKindAPIs)] = ptrFact(apisFact(t, "repo-billing", true))
	if err := svc.Reconcile(context.Background(), testOrgID); err != nil {
		t.Fatalf("Reconcile() error = %v, want nil", err)
	}

	live := store.liveAPIs()
	if len(live) != 1 || live[0].RepositoryID != "repo-orders" {
		t.Errorf("live apis = %+v, want only orders' spec kept while billing's is swept", live)
	}
}

func ptrFact(fact models.RepositoryFact) *models.RepositoryFact { return &fact }

func TestAPIDiscovery_RespectsSuppression(t *testing.T) {
	store := newArchitectureStore()
	store.suppressions = []models.DerivationSuppression{{
		OrganizationID:        testOrgID,
		DerivationKey:         "apidisc:v1:repo/repo-orders",
		DerivationFingerprint: Fingerprint(derive.RuleAPIRootMarker, "examples/openapi.yaml"),
		Reason:                "example spec, not a real contract",
	}}
	svc := steppingService(store)

	reconcileAPIsWith(t, store, svc, apisFact(t, "repo-orders", true,
		spec("openapi.yaml", "Orders API", "1.0.0"),
		spec("examples/openapi.yaml", "Example", "1.0.0")))

	live := store.liveAPIs()
	if len(live) != 1 || live[0].SpecPath != "openapi.yaml" {
		t.Errorf("live apis = %+v, want only the unsuppressed spec", live)
	}
}
