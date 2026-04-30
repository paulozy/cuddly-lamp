package dependencies

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Package struct {
	Name      string
	Version   string
	Ecosystem string
	IsDirect  bool
}

var ManifestFiles = map[string]func(string) []Package{
	"go.mod":           ParseGoMod,
	"package.json":     ParsePackageJSON,
	"requirements.txt": ParseRequirementsTxt,
	"Cargo.toml":       ParseCargoToml,
}

func IsManifestFile(path string) bool {
	_, ok := ManifestFiles[path]
	return ok
}

func ParseGoMod(content string) []Package {
	var packages []Package
	inRequireBlock := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "require (") {
			inRequireBlock = true
			continue
		}
		if inRequireBlock && line == ")" {
			inRequireBlock = false
			continue
		}
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		} else if !inRequireBlock {
			continue
		}
		pkg, ok := parseGoRequireLine(line)
		if ok {
			packages = append(packages, pkg)
		}
	}
	return packages
}

func parseGoRequireLine(line string) (Package, bool) {
	isDirect := !strings.Contains(line, "// indirect")
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return Package{}, false
	}
	return Package{Name: parts[0], Version: parts[1], Ecosystem: "go", IsDirect: isDirect}, true
}

func ParsePackageJSON(content string) []Package {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil
	}
	packages := make([]Package, 0, len(manifest.Dependencies)+len(manifest.DevDependencies))
	for name, version := range manifest.Dependencies {
		packages = append(packages, Package{Name: name, Version: version, Ecosystem: "npm", IsDirect: true})
	}
	for name, version := range manifest.DevDependencies {
		packages = append(packages, Package{Name: name, Version: version, Ecosystem: "npm", IsDirect: false})
	}
	return packages
}

var requirementPattern = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)\s*(?:==|>=|<=|~=|!=|>|<)\s*([^#;\s]+)`)

func ParseRequirementsTxt(content string) []Package {
	var packages []Package
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		matches := requirementPattern.FindStringSubmatch(line)
		if len(matches) == 3 {
			packages = append(packages, Package{Name: matches[1], Version: matches[2], Ecosystem: "pip", IsDirect: true})
		}
	}
	return packages
}

func ParseCargoToml(content string) []Package {
	var manifest struct {
		Dependencies map[string]any `toml:"dependencies"`
	}
	if err := toml.Unmarshal([]byte(content), &manifest); err != nil {
		return nil
	}
	packages := make([]Package, 0, len(manifest.Dependencies))
	for name, raw := range manifest.Dependencies {
		version := ""
		switch v := raw.(type) {
		case string:
			version = v
		case map[string]any:
			if s, ok := v["version"].(string); ok {
				version = s
			}
		}
		packages = append(packages, Package{Name: name, Version: version, Ecosystem: "cargo", IsDirect: true})
	}
	return packages
}
