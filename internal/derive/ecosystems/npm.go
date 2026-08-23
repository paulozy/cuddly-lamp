package ecosystems

import (
	"encoding/json"
	"fmt"
)

// packageJSON is the shallow slice of package.json this reader needs.
//
// `repository` is `any` because npm accepts three shapes for it — an object, a
// full URL string, and the `org/repo` shorthand — and a typed field would fail
// to decode two of them, taking the rest of the manifest down with it.
type packageJSON struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Private          bool              `json:"private"`
	Dependencies     map[string]string `json:"dependencies"`
	DevDependencies  map[string]string `json:"devDependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	Repository       any               `json:"repository"`
}

// ParseNPMPackage reads a package.json for its name and direct dependencies.
//
// All three dependency maps count. devDependencies especially: a repository that
// consumes an internal test helper or eslint config declares it there, and
// dropping them would hide a real internal edge.
//
// A manifest with no `name` is not an error — a private application root
// legitimately omits it. It publishes nothing, so it contributes only its
// requirements to the index.
func ParseNPMPackage(path string, content []byte) (Manifest, error) {
	var pkg packageJSON
	if err := json.Unmarshal(content, &pkg); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}

	manifest := Manifest{Path: path, Ecosystem: NPM}
	if pkg.Name != "" {
		manifest.Published = append(manifest.Published, Package{
			Ecosystem: NPM,
			Name:      Normalize(NPM, pkg.Name),
			RawName:   pkg.Name,
		})
	}
	for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies, pkg.PeerDependencies} {
		for name, version := range deps {
			if name == "" {
				continue
			}
			manifest.Requires = append(manifest.Requires, Package{
				Ecosystem: NPM,
				Name:      Normalize(NPM, name),
				RawName:   name,
				Version:   version,
			})
		}
	}
	manifest.RepositoryURL = npmRepositoryURL(pkg.Repository)
	return manifest, nil
}

func npmRepositoryURL(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case map[string]any:
		url, _ := value["url"].(string)
		return url
	default:
		return ""
	}
}
