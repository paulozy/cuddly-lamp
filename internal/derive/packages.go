package derive

import (
	"github.com/paulozy/idp-with-ai-backend/internal/derive/ecosystems"
)

// Rule ids for the library-dependency family. Stable strings: they are part of
// every fingerprint, so renaming one re-keys the graph.
const (
	RuleLibDepGo     = "libdep.gomod"
	RuleLibDepNPM    = "libdep.npm"
	RuleLibDepMaven  = "libdep.maven"
	RuleLibDepPython = "libdep.python"
	RuleLibDepCargo  = "libdep.cargo"
	RuleLibDepNuGet  = "libdep.nuget"
)

// RuleForEcosystem maps an ecosystem to the rule id its edges carry.
func RuleForEcosystem(ecosystem string) string {
	switch ecosystem {
	case ecosystems.Go:
		return RuleLibDepGo
	case ecosystems.NPM:
		return RuleLibDepNPM
	case ecosystems.Maven:
		return RuleLibDepMaven
	case ecosystems.Python:
		return RuleLibDepPython
	case ecosystems.Cargo:
		return RuleLibDepCargo
	case ecosystems.NuGet:
		return RuleLibDepNuGet
	default:
		return "libdep.unknown"
	}
}

// PackagesFact is the payload of a `packages` fact: everything one repository
// publishes and everything it directly declares.
//
// The two halves are deliberately flat rather than nested per manifest: the
// index joins on (ecosystem, name) and needs the manifest path only as evidence,
// which each entry already carries.
type PackagesFact struct {
	Published []PublishedPackage `json:"published,omitempty"`
	Requires  []RequiredPackage  `json:"requires,omitempty"`
	// ManifestCount is how many manifests were read, for the evaluation gate at
	// the end of phase 1: "how many manifests could the extractor not read".
	ManifestCount int `json:"manifest_count"`
}

// PublishedPackage is one name this repository publishes, with the manifest that
// says so.
type PublishedPackage struct {
	Ecosystem    string `json:"ecosystem"`
	Name         string `json:"name"`
	RawName      string `json:"raw_name,omitempty"`
	ManifestPath string `json:"manifest_path"`
	PURL         string `json:"purl,omitempty"`
}

// RequiredPackage is one dependency this repository declares.
type RequiredPackage struct {
	Ecosystem    string `json:"ecosystem"`
	Name         string `json:"name"`
	RawName      string `json:"raw_name,omitempty"`
	ManifestPath string `json:"manifest_path"`
	// Version is display-only and is deliberately excluded from the fingerprint:
	// a version bump must not retract and recreate the edge.
	Version string `json:"version,omitempty"`
	PURL    string `json:"purl,omitempty"`
}

// PackagesFromManifests folds shallow manifest readings into a fact payload.
//
// It is a pure function over already-parsed manifests, which is what lets the
// index tests work on literals with no provider anywhere in sight.
func PackagesFromManifests(manifests []ecosystems.Manifest) PackagesFact {
	fact := PackagesFact{ManifestCount: len(manifests)}
	for _, manifest := range manifests {
		for _, pkg := range manifest.Published {
			fact.Published = append(fact.Published, PublishedPackage{
				Ecosystem:    pkg.Ecosystem,
				Name:         pkg.Name,
				RawName:      pkg.RawName,
				ManifestPath: manifest.Path,
				PURL:         ecosystems.PURL(pkg.Ecosystem, pkg.RawName, ""),
			})
		}
		for _, pkg := range manifest.Requires {
			fact.Requires = append(fact.Requires, RequiredPackage{
				Ecosystem:    pkg.Ecosystem,
				Name:         pkg.Name,
				RawName:      pkg.RawName,
				ManifestPath: manifest.Path,
				Version:      pkg.Version,
				PURL:         ecosystems.PURL(pkg.Ecosystem, pkg.RawName, pkg.Version),
			})
		}
	}
	return fact
}
