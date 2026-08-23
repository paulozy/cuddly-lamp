package ecosystems

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type pyproject struct {
	Project struct {
		Name         string            `toml:"name"`
		Version      string            `toml:"version"`
		Dependencies []string          `toml:"dependencies"`
		URLs         map[string]string `toml:"urls"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Name         string         `toml:"name"`
			Repository   string         `toml:"repository"`
			Dependencies map[string]any `toml:"dependencies"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

// ParsePyProject reads a pyproject.toml for its distribution name and direct
// dependencies.
//
// Both layouts are read: PEP 621's `[project]` and Poetry's
// `[tool.poetry]`. They coexist in the wild and a reader that knows only one
// reports a real package as publishing nothing.
//
// Poetry's dependency table always contains `python`, which is the interpreter
// constraint and not a package; it is dropped so it cannot end up in the index.
func ParsePyProject(path string, content []byte) (Manifest, error) {
	var doc pyproject
	if err := toml.Unmarshal(content, &doc); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}

	manifest := Manifest{Path: path, Ecosystem: Python}

	name := doc.Project.Name
	if name == "" {
		name = doc.Tool.Poetry.Name
	}
	if name != "" {
		manifest.Published = append(manifest.Published, Package{
			Ecosystem: Python,
			Name:      Normalize(Python, name),
			RawName:   name,
		})
	}

	manifest.RepositoryURL = doc.Tool.Poetry.Repository
	if manifest.RepositoryURL == "" {
		for _, key := range []string{"Repository", "repository", "Source", "source", "Homepage"} {
			if url, ok := doc.Project.URLs[key]; ok && url != "" {
				manifest.RepositoryURL = url
				break
			}
		}
	}

	for _, spec := range doc.Project.Dependencies {
		if pkg, ok := parsePEP508(spec); ok {
			manifest.Requires = append(manifest.Requires, pkg)
		}
	}
	for name := range doc.Tool.Poetry.Dependencies {
		if name == "" || strings.EqualFold(name, "python") {
			continue
		}
		manifest.Requires = append(manifest.Requires, Package{
			Ecosystem: Python,
			Name:      Normalize(Python, name),
			RawName:   name,
		})
	}
	return manifest, nil
}

// ParseRequirements reads a requirements.txt for its direct dependencies.
//
// It publishes nothing by definition — requirements.txt is a consumption list.
//
// Lines beginning with `-r`, `-c`, `-e` or `--` are options, not packages: `-r`
// includes another file (which the caller will read on its own if it is in the
// tree), and `-e` is an editable local checkout. Treating either as a package
// name puts a file path into the index.
func ParseRequirements(path string, content []byte) (Manifest, error) {
	manifest := Manifest{Path: path, Ecosystem: Python}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	// Requirements files are small, but a committed one can hold a long pinned
	// tree; a generous line cap keeps a single long line from ending the scan.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		// An inline comment must not become part of the name.
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if pkg, ok := parsePEP508(line); ok {
			manifest.Requires = append(manifest.Requires, pkg)
		}
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return manifest, nil
}

// pep508Terminators are every character that can end a PEP 508 distribution
// name: extras, version specifiers, and the environment-marker separator.
const pep508Terminators = "[=<>!~;@ \t,("

// parsePEP508 takes the distribution name off the front of a requirement
// specifier. `pkg[extra]>=1.0; python_version < "3.11"` yields `pkg`.
func parsePEP508(spec string) (Package, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Package{}, false
	}
	end := strings.IndexAny(spec, pep508Terminators)
	name := spec
	version := ""
	if end >= 0 {
		name = strings.TrimSpace(spec[:end])
		version = strings.TrimSpace(spec[end:])
	}
	if name == "" {
		return Package{}, false
	}
	return Package{
		Ecosystem: Python,
		Name:      Normalize(Python, name),
		RawName:   name,
		Version:   version,
	}, true
}
