package ecosystems

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

type cargoToml struct {
	Package struct {
		Name       string `toml:"name"`
		Version    string `toml:"version"`
		Repository string `toml:"repository"`
	} `toml:"package"`
	// Dependency values are either a version string or an inline table, so the
	// value type has to be `any` or the whole file fails to decode.
	Dependencies      map[string]any `toml:"dependencies"`
	DevDependencies   map[string]any `toml:"dev-dependencies"`
	BuildDependencies map[string]any `toml:"build-dependencies"`
}

// ParseCargoToml reads a Cargo.toml for its crate name and direct dependencies.
//
// A workspace root has `[workspace]` and no `[package]`, so it publishes nothing
// and only points at its members — the same shape as an npm workspace root.
func ParseCargoToml(path string, content []byte) (Manifest, error) {
	var doc cargoToml
	if err := toml.Unmarshal(content, &doc); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}

	manifest := Manifest{
		Path:          path,
		Ecosystem:     Cargo,
		RepositoryURL: doc.Package.Repository,
	}
	if doc.Package.Name != "" {
		manifest.Published = append(manifest.Published, Package{
			Ecosystem: Cargo,
			Name:      Normalize(Cargo, doc.Package.Name),
			RawName:   doc.Package.Name,
		})
	}
	for _, deps := range []map[string]any{doc.Dependencies, doc.DevDependencies, doc.BuildDependencies} {
		for name, spec := range deps {
			if name == "" {
				continue
			}
			manifest.Requires = append(manifest.Requires, Package{
				Ecosystem: Cargo,
				Name:      Normalize(Cargo, name),
				RawName:   name,
				Version:   cargoVersion(spec),
			})
		}
	}
	return manifest, nil
}

func cargoVersion(spec any) string {
	switch value := spec.(type) {
	case string:
		return value
	case map[string]any:
		version, _ := value["version"].(string)
		return version
	default:
		return ""
	}
}
