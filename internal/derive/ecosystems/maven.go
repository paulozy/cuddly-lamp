package ecosystems

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type pomXML struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Parent     struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
	} `xml:"parent"`
	SCM struct {
		URL string `xml:"url"`
	} `xml:"scm"`
	Dependencies struct {
		Dependency []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
			Scope      string `xml:"scope"`
		} `xml:"dependency"`
	} `xml:"dependencies"`
}

// ParseMavenPOM reads a pom.xml for its coordinate and direct dependencies.
//
// The `groupId` may be inherited from the parent POM, which is legal and common
// in a multi-module build, so the parent's is used as the fallback.
//
// Property interpolation is the accepted limitation, and it is handled by
// refusing rather than guessing: a coordinate containing `${` (the `${revision}`
// pattern, or any inherited property) cannot be resolved by a shallow read, and
// an unresolved placeholder in an index would either match nothing or — worse —
// match another manifest with the same placeholder. Dropping it costs a possible
// edge; keeping it invents one at confidence 1.00, which is the worst outcome
// available.
func ParseMavenPOM(path string, content []byte) (Manifest, error) {
	var pom pomXML
	if err := xml.Unmarshal(content, &pom); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}

	manifest := Manifest{Path: path, Ecosystem: Maven, RepositoryURL: pom.SCM.URL}

	group := pom.GroupID
	if group == "" {
		group = pom.Parent.GroupID
	}
	if coordinate, ok := mavenCoordinate(group, pom.ArtifactID); ok {
		manifest.Published = append(manifest.Published, Package{
			Ecosystem: Maven,
			Name:      Normalize(Maven, coordinate),
			RawName:   coordinate,
		})
	}
	for _, dep := range pom.Dependencies.Dependency {
		coordinate, ok := mavenCoordinate(dep.GroupID, dep.ArtifactID)
		if !ok {
			continue
		}
		manifest.Requires = append(manifest.Requires, Package{
			Ecosystem: Maven,
			Name:      Normalize(Maven, coordinate),
			RawName:   coordinate,
			Version:   dep.Version,
		})
	}
	return manifest, nil
}

// mavenCoordinate joins a groupId and artifactId, refusing anything still
// carrying a property placeholder.
func mavenCoordinate(group, artifact string) (string, bool) {
	group, artifact = strings.TrimSpace(group), strings.TrimSpace(artifact)
	if group == "" || artifact == "" {
		return "", false
	}
	if strings.Contains(group, "${") || strings.Contains(artifact, "${") {
		return "", false
	}
	return group + ":" + artifact, true
}
