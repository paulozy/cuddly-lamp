package ecosystems

import (
	"fmt"

	"golang.org/x/mod/modfile"
)

// ParseGoMod reads a go.mod for its module path and its direct requirements.
//
// `golang.org/x/mod/modfile` is the same parser the Go toolchain uses, so block
// and single-line `require` forms, comments, and `// indirect` markers all come
// out correct without a hand-written scanner. It is already in go.sum as a
// transitive dependency of x/tools, so this costs no new supply-chain surface.
//
// Go is the ecosystem that comes out best here: `module github.com/org/repo`
// literally encodes the repository, so the published name and the repository URL
// can corroborate each other.
//
// `replace` and `exclude` are deliberately ignored. A replace directive points a
// module at a local path or a fork for build purposes; treating it as a declared
// dependency would put a `../shared` or a fork's path into the index, and
// neither is what the repository publishes or consumes in the graph sense.
func ParseGoMod(path string, content []byte) (Manifest, error) {
	// Lax parsing: a go.mod with a directive this version of x/mod does not know
	// still yields the module path and requirements, which is all we read.
	file, err := modfile.ParseLax(path, content, nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}

	manifest := Manifest{Path: path, Ecosystem: Go}
	if file.Module != nil && file.Module.Mod.Path != "" {
		manifest.Published = append(manifest.Published, Package{
			Ecosystem: Go,
			Name:      Normalize(Go, file.Module.Mod.Path),
			RawName:   file.Module.Mod.Path,
		})
		// The module path is the repository URL for anything hosted on a forge,
		// which is what makes the Go corroboration free.
		manifest.RepositoryURL = "https://" + file.Module.Mod.Path
	}
	for _, req := range file.Require {
		if req == nil || req.Mod.Path == "" {
			continue
		}
		manifest.Requires = append(manifest.Requires, Package{
			Ecosystem: Go,
			Name:      Normalize(Go, req.Mod.Path),
			RawName:   req.Mod.Path,
			Version:   req.Mod.Version,
		})
	}
	return manifest, nil
}
