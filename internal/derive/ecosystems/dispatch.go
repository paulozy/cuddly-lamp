package ecosystems

import (
	"path"
	"strings"
)

// Parser reads one manifest's bytes into a shallow Manifest.
type Parser func(path string, content []byte) (Manifest, error)

// ParserFor returns the reader for a path, or nil when the path is not a
// manifest this package understands.
//
// Matching is on the base name, and the caller is responsible for having already
// dropped vendored trees — `node_modules/lodash/package.json` is a real
// package.json and this function cannot tell that it is not ours. That filter
// lives in one place (detect.IsVendored) precisely so it is not re-implemented
// here and allowed to drift.
func ParserFor(filePath string) Parser {
	base := path.Base(filePath)
	switch base {
	case "go.mod":
		return ParseGoMod
	case "package.json":
		return ParseNPMPackage
	case "pom.xml":
		return ParseMavenPOM
	case "pyproject.toml":
		return ParsePyProject
	case "Cargo.toml":
		return ParseCargoToml
	}
	// requirements.txt and its conventional variants are all consumption lists
	// with the same syntax: `requirements-dev.txt` next to the root one, and
	// `requirements/base.txt` when a project splits them into a directory. The
	// second form is why the parent directory is checked too — matching only the
	// base name misses the split layout entirely.
	if strings.HasSuffix(base, ".txt") &&
		(strings.HasPrefix(base, "requirements") || path.Base(path.Dir(filePath)) == "requirements") {
		return ParseRequirements
	}
	if strings.HasSuffix(base, ".csproj") {
		return ParseCsproj
	}
	return nil
}

// IsManifest reports whether a path is a manifest this package can read.
func IsManifest(filePath string) bool {
	return ParserFor(filePath) != nil
}
