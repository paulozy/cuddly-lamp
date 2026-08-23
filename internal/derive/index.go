package derive

import "sort"

// PackageIndex maps a published package name to the repository that publishes
// it.
//
// This is the piece that makes library edges *authoritative* rather than
// heuristic: we own the repositories, so an index built from their own manifests
// is a statement of fact, not a guess. A dependency that does not appear in it is
// simply external and produces nothing — there is no fallback that guesses.
//
// Prior art says the general package↔project problem is hard: Google built
// deps.dev as a whole service for it, and the field it exposes is an *ordinal
// provenance enum* (SLSA_ATTESTATION, GO_ORIGIN, … UNVERIFIED_METADATA), not a
// score. The model is worth copying; the service is not, because it covers only
// public registries and this entire feature is about an organization's private
// packages. GitHub's own package→repo answer, `network/dependents`, notoriously
// has no API.
type PackageIndex struct {
	// owners maps identity to every repository claiming it. Plural because
	// ambiguity has to be *detected*, which a map to a single id cannot do.
	owners map[packageIdentity]map[string]bool
}

type packageIdentity struct {
	ecosystem string
	name      string
}

func NewPackageIndex() *PackageIndex {
	return &PackageIndex{owners: make(map[packageIdentity]map[string]bool)}
}

// Add records that repositoryID publishes (ecosystem, name).
//
// Many-to-one is the normal case and needs no special handling: a monorepo
// publishing five packages simply adds five entries pointing at itself. That is
// how monorepos are supported without any attempt to split them into
// Components — which is explicitly out of scope.
func (idx *PackageIndex) Add(ecosystem, name, repositoryID string) {
	if ecosystem == "" || name == "" || repositoryID == "" {
		return
	}
	identity := packageIdentity{ecosystem: ecosystem, name: name}
	if idx.owners[identity] == nil {
		idx.owners[identity] = make(map[string]bool, 1)
	}
	idx.owners[identity][repositoryID] = true
}

// Resolve returns the repository publishing a package.
//
// Ambiguous is true when more than one repository claims the same name — a fork,
// or two repositories that genuinely both publish it. In that case ownerID is
// empty and the caller must create no edge. Picking one would produce a *wrong*
// edge at confidence 1.00, which is the worst possible outcome: a high-confidence
// lie is harder to notice than a missing edge and destroys trust in the catalog.
func (idx *PackageIndex) Resolve(ecosystem, name string) (ownerID string, ambiguous bool, found bool) {
	owners := idx.owners[packageIdentity{ecosystem: ecosystem, name: name}]
	switch len(owners) {
	case 0:
		return "", false, false
	case 1:
		for id := range owners {
			return id, false, true
		}
		return "", false, false
	default:
		return "", true, true
	}
}

// Ambiguity is a package name more than one repository claims.
//
// It is recorded rather than silently dropped so the UI can say "two
// repositories claim the package `X`; resolve it by declaring the relationship
// by hand" — which is an actionable message, unlike a missing edge.
type Ambiguity struct {
	Ecosystem string   `json:"ecosystem"`
	Name      string   `json:"name"`
	Claimants []string `json:"claimants"`
}

// Ambiguities lists every contested name, sorted so the output is stable.
func (idx *PackageIndex) Ambiguities() []Ambiguity {
	var out []Ambiguity
	for identity, owners := range idx.owners {
		if len(owners) < 2 {
			continue
		}
		claimants := make([]string, 0, len(owners))
		for id := range owners {
			claimants = append(claimants, id)
		}
		sort.Strings(claimants)
		out = append(out, Ambiguity{Ecosystem: identity.ecosystem, Name: identity.name, Claimants: claimants})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Len is how many distinct package identities the index holds.
func (idx *PackageIndex) Len() int { return len(idx.owners) }
