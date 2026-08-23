package ecosystems

import (
	"encoding/xml"
	"fmt"
	"path"
	"strings"
)

type csproj struct {
	PropertyGroup []struct {
		PackageID     string `xml:"PackageId"`
		AssemblyName  string `xml:"AssemblyName"`
		RepositoryURL string `xml:"RepositoryUrl"`
	} `xml:"PropertyGroup"`
	ItemGroup []struct {
		PackageReference []struct {
			Include string `xml:"Include,attr"`
			Version string `xml:"Version,attr"`
		} `xml:"PackageReference"`
	} `xml:"ItemGroup"`
}

// ParseCsproj reads a .csproj for its package id and its PackageReferences.
//
// The published name falls back through `PackageId`, then `AssemblyName`, then
// the file's own base name — which is what MSBuild itself does when neither
// property is set, and is correct for the overwhelming majority of projects that
// set neither.
func ParseCsproj(filePath string, content []byte) (Manifest, error) {
	var doc csproj
	if err := xml.Unmarshal(content, &doc); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", filePath, err)
	}

	manifest := Manifest{Path: filePath, Ecosystem: NuGet}

	name := ""
	for _, group := range doc.PropertyGroup {
		if manifest.RepositoryURL == "" {
			manifest.RepositoryURL = strings.TrimSpace(group.RepositoryURL)
		}
		if name == "" {
			if id := strings.TrimSpace(group.PackageID); id != "" {
				name = id
			} else if assembly := strings.TrimSpace(group.AssemblyName); assembly != "" {
				name = assembly
			}
		}
	}
	if name == "" {
		name = strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))
	}
	if name != "" && !strings.Contains(name, "$(") {
		manifest.Published = append(manifest.Published, Package{
			Ecosystem: NuGet,
			Name:      Normalize(NuGet, name),
			RawName:   name,
		})
	}

	for _, group := range doc.ItemGroup {
		for _, ref := range group.PackageReference {
			include := strings.TrimSpace(ref.Include)
			// MSBuild property syntax is the NuGet equivalent of Maven's
			// `${revision}`: unresolvable in a shallow read, so refused.
			if include == "" || strings.Contains(include, "$(") {
				continue
			}
			manifest.Requires = append(manifest.Requires, Package{
				Ecosystem: NuGet,
				Name:      Normalize(NuGet, include),
				RawName:   include,
				Version:   ref.Version,
			})
		}
	}
	return manifest, nil
}
