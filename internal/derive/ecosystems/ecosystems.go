// Package ecosystems holds one shallow manifest reader per packaging
// ecosystem.
//
// Every reader answers exactly two questions: what does this manifest *publish*,
// and what does it *directly declare* a dependency on. That is all an internal
// edge needs, and it is what makes the whole problem small: no lockfile, no
// version resolution, no transitive closure, no SemVer. "N manifest parsers"
// becomes "N shallow key readers", most of them under forty lines.
//
// Normalization is per ecosystem and deliberately ours rather than purl's. purl
// (ECMA-427) is a fine display and export format, but its own spec disqualifies
// it as a join key — the `golang` type definition says it "predates Go modules
// and has several practical problems, and in particular it is impossible to
// determine what is a module and what is a package short of having full access
// to the source code or making an API call to the Go module proxy", and it
// declares `case_sensitive: true` alongside `"The namespace shall be
// lowercased"`, which is lossy for `github.com/BurntSushi/toml`.
package ecosystems

import "strings"

// Ecosystem names. Stable strings: they are half of every package identity and
// part of every fingerprint, so renaming one re-keys the graph.
const (
	Go     = "go"
	NPM    = "npm"
	Maven  = "maven"
	Python = "python"
	Cargo  = "cargo"
	NuGet  = "nuget"
)

// Package is one name a manifest publishes or depends on.
type Package struct {
	Ecosystem string `json:"ecosystem"`
	// Name is normalized for joining; see Normalize.
	Name string `json:"name"`
	// RawName is what the manifest actually said, kept for display.
	RawName string `json:"raw_name,omitempty"`
	// Version is the declared constraint, for display only. It is deliberately
	// never part of a fingerprint: a version bump must not retract and recreate
	// the edge.
	Version string `json:"version,omitempty"`
}

// Manifest is one file's shallow reading.
type Manifest struct {
	Path      string `json:"path"`
	Ecosystem string `json:"ecosystem"`
	// Published are the names this manifest claims to publish. Plural because a
	// Maven POM publishes one coordinate while an npm workspace root publishes
	// none and points at children.
	Published []Package `json:"published,omitempty"`
	// Requires are the directly declared dependencies. Transitive ones are
	// deliberately absent — an internal edge needs only what this repository
	// itself declares.
	Requires []Package `json:"requires,omitempty"`
	// RepositoryURL is the manifest's own claim about where its source lives.
	// Corroboration only: it is never the join key, because the index built from
	// the organization's own repositories is already authoritative.
	RepositoryURL string `json:"repository_url,omitempty"`
	// Deliberately absent: the workspace globs (npm `workspaces`, Cargo
	// `[workspace] members`, Maven `<modules>`). They exist to tell a reader
	// where a monorepo's sub-manifests are — and the caller already walks the
	// whole tree, so it finds `packages/a/package.json` without being pointed at
	// it. Following the globs as well would be a second, weaker path to the same
	// files, and the tree walk is the one that also catches a sub-package the
	// glob list forgot.
}

// Normalize folds a name into the form the index joins on.
//
// Each rule comes from the ecosystem's own case semantics, not from a
// preference:
//
//   - Go module paths are case-sensitive except the host, and
//     `github.com/BurntSushi/toml` is a real module whose capitals matter.
//   - npm forbids uppercase in new names, so lowercasing is lossless.
//   - Maven coordinates are case-sensitive.
//   - Python normalizes per PEP 503: lowercase, and `_` and `.` both fold to `-`.
//   - Cargo treats `_` and `-` as equivalent and is case-insensitive.
//   - NuGet ids are compared case-insensitively.
func Normalize(ecosystem, name string) string {
	name = strings.TrimSpace(name)
	switch ecosystem {
	case Go:
		host, rest, found := strings.Cut(name, "/")
		if !found {
			return strings.ToLower(name)
		}
		return strings.ToLower(host) + "/" + rest
	case NPM:
		return strings.ToLower(name)
	case Maven:
		return name
	case Python:
		lowered := strings.ToLower(name)
		return strings.NewReplacer("_", "-", ".", "-").Replace(lowered)
	case Cargo:
		return strings.ReplaceAll(strings.ToLower(name), "_", "-")
	case NuGet:
		return strings.ToLower(name)
	default:
		return strings.ToLower(name)
	}
}

// PURL renders a package as a Package URL string, for display and export.
//
// It is derived *from* the identity and never used as one. Fifteen lines of
// string building is a better trade than a dependency for a format we only
// print — see the package comment for why it cannot be the join key.
func PURL(ecosystem, name, version string) string {
	var typ, namespace, base string
	switch ecosystem {
	case Go:
		typ = "golang"
		if idx := strings.LastIndex(name, "/"); idx > 0 {
			namespace, base = name[:idx], name[idx+1:]
		} else {
			base = name
		}
	case NPM:
		typ = "npm"
		if scope, rest, found := strings.Cut(name, "/"); found && strings.HasPrefix(scope, "@") {
			namespace, base = scope, rest
		} else {
			base = name
		}
	case Maven:
		typ = "maven"
		namespace, base, _ = strings.Cut(name, ":")
	case Python:
		typ = "pypi"
		base = name
	case Cargo:
		typ = "cargo"
		base = name
	case NuGet:
		typ = "nuget"
		base = name
	default:
		typ = ecosystem
		base = name
	}

	out := "pkg:" + typ + "/"
	if namespace != "" {
		out += namespace + "/"
	}
	out += base
	if version != "" {
		out += "@" + version
	}
	return out
}
