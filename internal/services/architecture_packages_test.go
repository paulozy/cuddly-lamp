package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/derive/ecosystems"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// packagesFact builds the stored row a repository's extraction would leave, so
// the deriver can be exercised as the pure function it is.
func packagesFact(t *testing.T, repositoryID string, complete bool, payload derive.PackagesFact) models.RepositoryFact {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"data":    payload,
		"outcome": derive.Outcome{Complete: complete},
	})
	if err != nil {
		t.Fatalf("marshal fact payload: %v", err)
	}
	return models.RepositoryFact{
		OrganizationID: testOrgID,
		RepositoryID:   repositoryID,
		FactKind:       models.FactKindPackages,
		Payload:        encoded,
		Complete:       complete,
	}
}

func published(ecosystem, name, manifestPath string) derive.PublishedPackage {
	return derive.PublishedPackage{Ecosystem: ecosystem, Name: name, RawName: name, ManifestPath: manifestPath}
}

func required(ecosystem, name, manifestPath, version string) derive.RequiredPackage {
	return derive.RequiredPackage{
		Ecosystem: ecosystem, Name: name, RawName: name,
		ManifestPath: manifestPath, Version: version,
	}
}

func deriveLib(t *testing.T, facts ...models.RepositoryFact) DerivedSet {
	t.Helper()
	set, err := libDepDeriver{}.Derive(context.Background(), testOrgID, facts)
	if err != nil {
		t.Fatalf("Derive() error = %v, want nil", err)
	}
	return set
}

// ── the deriver ──────────────────────────────────────────────────────────────

// Go is the ecosystem that comes out best: `module github.com/org/shared`
// literally encodes the repository, so the match is an exact name against the
// organization's own index and nothing is guessed.
func TestLibDep_DeclaredInternalModuleYieldsEdge(t *testing.T) {
	set := deriveLib(t,
		packagesFact(t, "repo-checkout", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/checkout", "go.mod")},
			Requires:  []derive.RequiredPackage{required(ecosystems.Go, "github.com/org/shared", "go.mod", "v1.2.0")},
		}),
		packagesFact(t, "repo-shared", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/shared", "go.mod")},
		}),
	)

	if len(set.Edges) != 1 {
		t.Fatalf("edges = %+v, want exactly one", set.Edges)
	}
	got := set.Edges[0]
	if got.SourceRepositoryID != "repo-checkout" || got.TargetRepositoryID != "repo-shared" {
		t.Errorf("edge = %s → %s, want repo-checkout → repo-shared", got.SourceRepositoryID, got.TargetRepositoryID)
	}
	if got.Kind != models.RepositoryRelationshipKindLibrary {
		t.Errorf("kind = %q, want %q", got.Kind, models.RepositoryRelationshipKindLibrary)
	}
	// `manifest` was in the CHECK from migration 012 and never used until now.
	if got.Source != models.RepositoryRelationshipSourceManifest {
		t.Errorf("source = %q, want %q", got.Source, models.RepositoryRelationshipSourceManifest)
	}
	if got.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1", got.Confidence)
	}
	if got.Metadata["declared_version"] != "v1.2.0" {
		t.Errorf("declared_version = %v, want v1.2.0", got.Metadata["declared_version"])
	}
	if !set.Complete {
		t.Error("Complete = false, want true — both facts were complete")
	}
}

// A dependency nothing in the organization publishes is external and produces
// nothing. There is no fallback that guesses.
func TestLibDep_ExternalDependencyYieldsNoEdge(t *testing.T) {
	set := deriveLib(t, packagesFact(t, "repo-checkout", true, derive.PackagesFact{
		Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/checkout", "go.mod")},
		Requires:  []derive.RequiredPackage{required(ecosystems.Go, "github.com/gin-gonic/gin", "go.mod", "v1.12.0")},
	}))

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none", set.Edges)
	}
}

// Two repositories claiming the same name — a fork, typically — must produce no
// edge at all. Guessing here yields a wrong edge at confidence 1.00, which is
// the worst outcome available.
func TestLibDep_AmbiguousPackageNameYieldsNoEdge(t *testing.T) {
	set := deriveLib(t,
		packagesFact(t, "repo-checkout", true, derive.PackagesFact{
			Requires: []derive.RequiredPackage{required(ecosystems.Go, "github.com/org/shared", "go.mod", "v1.2.0")},
		}),
		packagesFact(t, "repo-shared", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/shared", "go.mod")},
		}),
		packagesFact(t, "repo-shared-fork", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/shared", "go.mod")},
		}),
	)

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none while the name is contested", set.Edges)
	}
}

// A monorepo commonly declares a dependency on a package it publishes itself.
// Filtering it here keeps it away from the no_self_repository_relationship CHECK.
func TestLibDep_SelfDependencyYieldsNoEdge(t *testing.T) {
	set := deriveLib(t, packagesFact(t, "repo-platform", true, derive.PackagesFact{
		Published: []derive.PublishedPackage{
			published(ecosystems.NPM, "@org/ui", "packages/ui/package.json"),
			published(ecosystems.NPM, "@org/app", "packages/app/package.json"),
		},
		Requires: []derive.RequiredPackage{required(ecosystems.NPM, "@org/ui", "packages/app/package.json", "^1.0.0")},
	}))

	if len(set.Edges) != 0 {
		t.Errorf("edges = %+v, want none for an intra-repository dependency", set.Edges)
	}
}

// One repository's incomplete extraction withdraws the sweep for the whole
// organization, and that is correct rather than harsh: the index is org-wide, so
// a name this repository failed to publish hides an edge from every other
// repository to it.
func TestLibDep_IncompleteFactWithdrawsTheSweepForTheWholeOrganization(t *testing.T) {
	set := deriveLib(t,
		packagesFact(t, "repo-checkout", true, derive.PackagesFact{
			Requires: []derive.RequiredPackage{required(ecosystems.Go, "github.com/org/shared", "go.mod", "v1.2.0")},
		}),
		packagesFact(t, "repo-shared", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/shared", "go.mod")},
		}),
		packagesFact(t, "repo-truncated", false, derive.PackagesFact{}),
	)

	if set.Complete {
		t.Error("Complete = true, want false — one repository could not be fully inspected")
	}
	// The edges it *could* see are still asserted; only retraction is withheld.
	if len(set.Edges) != 1 {
		t.Errorf("edges = %+v, want the one observable edge to still be written", set.Edges)
	}
}

// The manifest path is part of the identity so a monorepo declaring the same
// dependency from two manifests yields two edges with two pieces of evidence,
// instead of one row flickering between them.
func TestLibDep_SameDependencyFromTwoManifestsYieldsTwoFingerprints(t *testing.T) {
	set := deriveLib(t,
		packagesFact(t, "repo-platform", true, derive.PackagesFact{
			Requires: []derive.RequiredPackage{
				required(ecosystems.NPM, "@org/shared", "packages/web/package.json", "^1.0.0"),
				required(ecosystems.NPM, "@org/shared", "packages/worker/package.json", "^1.0.0"),
			},
		}),
		packagesFact(t, "repo-shared", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.NPM, "@org/shared", "package.json")},
		}),
	)

	if len(set.Edges) != 2 {
		t.Fatalf("edges = %d, want 2 — one per manifest that declares it", len(set.Edges))
	}
	if set.Edges[0].Fingerprint == set.Edges[1].Fingerprint {
		t.Error("fingerprints are equal, want one per manifest path")
	}
}

// A version bump must not retract and recreate the edge: that would reset the
// row's id and break every deep link to it. The version therefore lives in
// metadata and never in the fingerprint.
func TestLibDep_VersionBumpKeepsTheSameFingerprint(t *testing.T) {
	before := deriveLib(t,
		packagesFact(t, "repo-checkout", true, derive.PackagesFact{
			Requires: []derive.RequiredPackage{required(ecosystems.Go, "github.com/org/shared", "go.mod", "v1.2.0")},
		}),
		packagesFact(t, "repo-shared", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/shared", "go.mod")},
		}),
	)
	after := deriveLib(t,
		packagesFact(t, "repo-checkout", true, derive.PackagesFact{
			Requires: []derive.RequiredPackage{required(ecosystems.Go, "github.com/org/shared", "go.mod", "v2.0.0")},
		}),
		packagesFact(t, "repo-shared", true, derive.PackagesFact{
			Published: []derive.PublishedPackage{published(ecosystems.Go, "github.com/org/shared", "go.mod")},
		}),
	)

	if len(before.Edges) != 1 || len(after.Edges) != 1 {
		t.Fatalf("edges before = %d, after = %d, want 1 each", len(before.Edges), len(after.Edges))
	}
	if before.Edges[0].Fingerprint != after.Edges[0].Fingerprint {
		t.Error("fingerprint changed with the version, want it stable across a bump")
	}
}

// The version segment of the key is the mechanism for changing matching logic
// visibly: bump it and the old key's rows are retired by their own sweep.
func TestLibDep_KeyCarriesTheDeriverVersionAndOrganizationScope(t *testing.T) {
	got := libDepDeriver{}.Key("org-42")
	if got != "libdep:v1:org/org-42" {
		t.Errorf("Key() = %q, want %q", got, "libdep:v1:org/org-42")
	}
}

// ── the extractor ────────────────────────────────────────────────────────────

func extractPackages(t *testing.T, in ExtractInput) (derive.PackagesFact, derive.Outcome) {
	t.Helper()
	payload, outcome := packagesExtractor{}.Extract(context.Background(), in)
	fact, ok := payload.(derive.PackagesFact)
	if !ok {
		t.Fatalf("payload type = %T, want derive.PackagesFact", payload)
	}
	return fact, outcome
}

func packagesInput(contents map[string]string, sizes map[string]int, truncated bool) ExtractInput {
	paths := make([]string, 0, len(contents))
	for path := range contents {
		paths = append(paths, path)
	}
	reader := &stubFileReader{contents: contents}
	return ExtractInput{
		RepositoryID: "repo-a",
		Paths:        paths,
		Sizes:        sizes,
		Truncated:    truncated,
		Fetch: func(ctx context.Context, path string) ([]byte, error) {
			return reader.GetFileContent(ctx, scm.RepoRef{}, "main", path)
		},
	}
}

func TestPackagesExtractor_ReadsEveryManifestInTheTree(t *testing.T) {
	fact, outcome := extractPackages(t, packagesInput(map[string]string{
		"go.mod":                   "module github.com/org/checkout\n\nrequire github.com/org/shared v1.2.0\n",
		"web/package.json":         `{"name":"@org/web","dependencies":{"@org/ui":"^1.0.0"}}`,
		"README.md":                "# not a manifest",
		"packages/ui/package.json": `{"name":"@org/ui"}`,
	}, nil, false))

	if !outcome.Complete {
		t.Errorf("outcome = %+v, want complete", outcome)
	}
	if fact.ManifestCount != 3 {
		t.Errorf("ManifestCount = %d, want 3", fact.ManifestCount)
	}
	wantPublished := map[string]bool{"github.com/org/checkout": true, "@org/web": true, "@org/ui": true}
	for _, pkg := range fact.Published {
		delete(wantPublished, pkg.Name)
	}
	if len(wantPublished) != 0 {
		t.Errorf("published names missing: %v", wantPublished)
	}
}

// Without this filter node_modules/lodash/package.json enters the index as if
// this repository published lodash. It is the single largest false-positive class
// in the whole phase.
func TestPackagesExtractor_IgnoresVendoredManifests(t *testing.T) {
	fact, _ := extractPackages(t, packagesInput(map[string]string{
		"package.json":                     `{"name":"@org/web"}`,
		"node_modules/lodash/package.json": `{"name":"lodash"}`,
		"vendor/github.com/x/y/go.mod":     "module github.com/x/y\n",
	}, nil, false))

	for _, pkg := range fact.Published {
		if pkg.Name == "lodash" || pkg.Name == "github.com/x/y" {
			t.Errorf("published = %+v, want vendored manifests ignored", fact.Published)
		}
	}
	if fact.ManifestCount != 1 {
		t.Errorf("ManifestCount = %d, want 1", fact.ManifestCount)
	}
}

// A truncated listing proves presence and never absence, so a manifest we never
// saw is indistinguishable from one that was deleted.
func TestPackagesExtractor_TruncatedTreeIsIncomplete(t *testing.T) {
	_, outcome := extractPackages(t, packagesInput(map[string]string{
		"go.mod": "module github.com/org/checkout\n",
	}, nil, true))

	if outcome.Complete {
		t.Error("outcome.Complete = true, want false for a truncated listing")
	}
	if len(outcome.Reasons) == 0 || outcome.Reasons[0] != derive.ReasonTreeTruncated {
		t.Errorf("reasons = %v, want tree_truncated", outcome.Reasons)
	}
}

// A malformed manifest means we genuinely do not know what the repository
// declares, so the fact loses its authority to delete — and, critically, the
// extractor does not panic and take the worker down with it.
func TestPackagesExtractor_MalformedManifestIsIncompleteNotAPanic(t *testing.T) {
	fact, outcome := extractPackages(t, packagesInput(map[string]string{
		"package.json": `{"name": `,
		"other/go.mod": "module github.com/org/other\n",
	}, nil, false))

	if outcome.Complete {
		t.Error("outcome.Complete = true, want false after a parse failure")
	}
	if !hasReason(outcome.Reasons, derive.ReasonParseFailed) {
		t.Errorf("reasons = %v, want parse_failed", outcome.Reasons)
	}
	// The readable manifest still contributes: one bad file must not lose the rest.
	if len(fact.Published) != 1 || fact.Published[0].Name != "github.com/org/other" {
		t.Errorf("published = %+v, want the readable manifest to survive", fact.Published)
	}
}

// Reading hundreds of manifests would turn one sync into hundreds of sequential
// requests. Marking the fact incomplete is what keeps the truncation honest: a
// silently short read would look complete and authorise a sweep that deletes the
// edges of every manifest past the cut.
func TestPackagesExtractor_TooManyManifestsMarksIncompleteInsteadOfFetchingAll(t *testing.T) {
	contents := make(map[string]string, maxManifestsPerRepo+10)
	for i := 0; i < maxManifestsPerRepo+10; i++ {
		contents[packagePath(i)] = `{"name":"@org/pkg-` + itoa(i) + `"}`
	}
	fact, outcome := extractPackages(t, packagesInput(contents, nil, false))

	if outcome.Complete {
		t.Error("outcome.Complete = true, want false above the manifest ceiling")
	}
	if !hasReason(outcome.Reasons, derive.ReasonTooManyCandidates) {
		t.Errorf("reasons = %v, want too_many_candidates", outcome.Reasons)
	}
	if fact.ManifestCount > maxManifestsPerRepo {
		t.Errorf("ManifestCount = %d, want at most %d", fact.ManifestCount, maxManifestsPerRepo)
	}
}

func hasReason(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func packagePath(i int) string { return "packages/p" + itoa(i) + "/package.json" }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// A rate-limited read is not "the file is gone". Collapsing the two is what would
// let a 429 sweep a correct edge away.
func TestPackagesExtractor_ReadFailureIsIncomplete(t *testing.T) {
	reader := &stubFileReader{err: scm.ErrRateLimited}
	_, outcome := extractPackages(t, ExtractInput{
		RepositoryID: "repo-a",
		Paths:        []string{"go.mod"},
		Fetch: func(ctx context.Context, path string) ([]byte, error) {
			return reader.GetFileContent(ctx, scm.RepoRef{}, "main", path)
		},
	})

	if outcome.Complete {
		t.Error("outcome.Complete = true, want false after a rate-limited read")
	}
	if !hasReason(outcome.Reasons, derive.ReasonReadFailed) {
		t.Errorf("reasons = %v, want read_failed", outcome.Reasons)
	}
}

// A manifest the tree listed but the host no longer serves is a race with a
// commit, not a failure to inspect — so the fact keeps its authority.
func TestPackagesExtractor_MissingManifestKeepsTheFactComplete(t *testing.T) {
	_, outcome := extractPackages(t, packagesInput(map[string]string{}, nil, false))
	if !outcome.Complete {
		t.Errorf("outcome = %+v, want complete when the only failure was absence", outcome)
	}

	reader := &stubFileReader{contents: map[string]string{}}
	_, outcome = extractPackages(t, ExtractInput{
		RepositoryID: "repo-a",
		Paths:        []string{"go.mod"},
		Fetch: func(ctx context.Context, path string) ([]byte, error) {
			return reader.GetFileContent(ctx, scm.RepoRef{}, "main", path)
		},
	})
	if !outcome.Complete {
		t.Errorf("outcome = %+v, want complete — ErrNotFound is absence, not failure", outcome)
	}
}

func TestPackagesExtractor_KindAndVersion(t *testing.T) {
	if got := (packagesExtractor{}).Kind(); got != models.FactKindPackages {
		t.Errorf("Kind() = %q, want %q", got, models.FactKindPackages)
	}
	if got := (packagesExtractor{}).Version(); got != 1 {
		t.Errorf("Version() = %d, want 1", got)
	}
}

// ── end to end through the service ───────────────────────────────────────────

// The whole two-pass design in one test: two repositories sync, extraction leaves
// facts, reconciliation joins them into an edge.
func TestArchitecture_ExtractThenReconcileProducesTheEdge(t *testing.T) {
	store := newArchitectureStore()
	svc := NewArchitectureService(store, newFakeEnqueuer())
	step := 0
	svc.now = func() time.Time {
		step++
		return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC).Add(time.Duration(step) * time.Hour)
	}

	checkout := &models.Repository{ID: "repo-checkout", OrganizationID: testOrgID}
	shared := &models.Repository{ID: "repo-shared", OrganizationID: testOrgID}

	checkoutReader := &stubFileReader{contents: map[string]string{
		"go.mod": "module github.com/org/checkout\n\nrequire github.com/org/shared v1.2.0\n",
	}}
	sharedReader := &stubFileReader{contents: map[string]string{
		"go.mod": "module github.com/org/shared\n",
	}}

	svc.ExtractFacts(context.Background(), checkout, checkoutReader, scm.RepoRef{}, "main",
		tree("sha-a", false, blob("go.mod", 200)))
	svc.ExtractFacts(context.Background(), shared, sharedReader, scm.RepoRef{}, "main",
		tree("sha-b", false, blob("go.mod", 100)))

	if err := svc.Reconcile(context.Background(), testOrgID); err != nil {
		t.Fatalf("Reconcile() error = %v, want nil", err)
	}

	live := store.live()
	if len(live) != 1 {
		t.Fatalf("live relationships = %d, want 1", len(live))
	}
	got := live[0]
	if got.SourceRepositoryID != "repo-checkout" || got.TargetRepositoryID != "repo-shared" {
		t.Errorf("edge = %s → %s, want repo-checkout → repo-shared", got.SourceRepositoryID, got.TargetRepositoryID)
	}
	if got.Label != "github.com/org/shared" {
		t.Errorf("label = %q, want the package name", got.Label)
	}
	if !strings.HasPrefix(*got.DerivationKey, "libdep:v1:org/") {
		t.Errorf("derivation_key = %q, want the libdep key", *got.DerivationKey)
	}
}
