package derive

import (
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/derive/ecosystems"
)

// ── the index ────────────────────────────────────────────────────────────────

// A monorepo is supported by doing nothing special: the index is many-to-one, so
// five published packages become five entries pointing at the same repository.
// No attempt is made to split it into Components — that is out of scope.
func TestIndex_MonorepoPublishingManyPackagesMapsAllToOneRepo(t *testing.T) {
	idx := NewPackageIndex()
	for _, name := range []string{"@org/ui", "@org/api-client", "@org/config"} {
		idx.Add(ecosystems.NPM, name, "repo-platform")
	}

	if idx.Len() != 3 {
		t.Errorf("index size = %d, want 3", idx.Len())
	}
	for _, name := range []string{"@org/ui", "@org/api-client", "@org/config"} {
		owner, ambiguous, found := idx.Resolve(ecosystems.NPM, name)
		if !found || ambiguous || owner != "repo-platform" {
			t.Errorf("Resolve(%q) = (%q, %v, %v), want (repo-platform, false, true)", name, owner, ambiguous, found)
		}
	}
}

// The most dangerous case in phase 1. A fork, or two repositories that genuinely
// both publish the name, must produce *no* edge — picking one yields a wrong edge
// at confidence 1.00, and a high-confidence lie is harder to notice than a
// missing edge and is what destroys trust in a catalog.
func TestIndex_AmbiguousPackageNameYieldsNoEdge(t *testing.T) {
	idx := NewPackageIndex()
	idx.Add(ecosystems.Go, "github.com/org/shared", "repo-original")
	idx.Add(ecosystems.Go, "github.com/org/shared", "repo-fork")

	owner, ambiguous, found := idx.Resolve(ecosystems.Go, "github.com/org/shared")
	if !found {
		t.Fatal("found = false, want true — the name is in the index, just contested")
	}
	if !ambiguous {
		t.Error("ambiguous = false, want true")
	}
	if owner != "" {
		t.Errorf("owner = %q, want empty so no edge is created", owner)
	}

	// The contest is recorded so the UI can ask a person to resolve it, which is
	// the only correct answer available.
	ambiguities := idx.Ambiguities()
	if len(ambiguities) != 1 {
		t.Fatalf("ambiguities = %v, want one", ambiguities)
	}
	if len(ambiguities[0].Claimants) != 2 {
		t.Errorf("claimants = %v, want both repositories named", ambiguities[0].Claimants)
	}
}

// A dependency nothing in the organization publishes is simply external. There is
// no fallback that guesses — a miss produces nothing.
func TestIndex_ExternalDependencyYieldsNoEdge(t *testing.T) {
	idx := NewPackageIndex()
	idx.Add(ecosystems.Go, "github.com/org/shared", "repo-a")

	_, _, found := idx.Resolve(ecosystems.Go, "github.com/gin-gonic/gin")
	if found {
		t.Error("found = true, want false for a package nobody in the org publishes")
	}
}

// The ecosystem is half the identity: `shared` on npm and `shared` on Cargo are
// different packages, and joining them would fabricate an edge.
func TestIndex_EcosystemIsPartOfIdentity(t *testing.T) {
	idx := NewPackageIndex()
	idx.Add(ecosystems.NPM, "shared", "repo-node")
	idx.Add(ecosystems.Cargo, "shared", "repo-rust")

	if owner, _, _ := idx.Resolve(ecosystems.NPM, "shared"); owner != "repo-node" {
		t.Errorf("npm owner = %q, want repo-node", owner)
	}
	if owner, _, _ := idx.Resolve(ecosystems.Cargo, "shared"); owner != "repo-rust" {
		t.Errorf("cargo owner = %q, want repo-rust", owner)
	}
	if got := idx.Ambiguities(); len(got) != 0 {
		t.Errorf("ambiguities = %v, want none", got)
	}
}

// ── the fact payload ─────────────────────────────────────────────────────────

func TestPackagesFromManifests(t *testing.T) {
	manifests := []ecosystems.Manifest{
		{
			Path:      "go.mod",
			Ecosystem: ecosystems.Go,
			Published: []ecosystems.Package{{Ecosystem: ecosystems.Go, Name: "github.com/org/checkout", RawName: "github.com/org/checkout"}},
			Requires: []ecosystems.Package{
				{Ecosystem: ecosystems.Go, Name: "github.com/org/shared", RawName: "github.com/org/shared", Version: "v1.2.0"},
			},
		},
	}
	fact := PackagesFromManifests(manifests)

	if fact.ManifestCount != 1 {
		t.Errorf("ManifestCount = %d, want 1", fact.ManifestCount)
	}
	if len(fact.Published) != 1 || fact.Published[0].ManifestPath != "go.mod" {
		t.Fatalf("published = %+v, want one entry citing go.mod", fact.Published)
	}
	if len(fact.Requires) != 1 {
		t.Fatalf("requires = %+v, want one entry", fact.Requires)
	}
	// The declared version travels as display metadata, never as identity.
	if fact.Requires[0].Version != "v1.2.0" {
		t.Errorf("version = %q, want v1.2.0", fact.Requires[0].Version)
	}
	if fact.Requires[0].PURL != "pkg:golang/github.com/org/shared@v1.2.0" {
		t.Errorf("purl = %q, want the golang purl", fact.Requires[0].PURL)
	}
}
