package ecosystems

import (
	"testing"
)

// names returns the normalized names of a package slice, for compact asserts.
func names(pkgs []Package) []string {
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, pkg.Name)
	}
	return out
}

func hasName(pkgs []Package, want string) bool {
	for _, pkg := range pkgs {
		if pkg.Name == want {
			return true
		}
	}
	return false
}

func single(t *testing.T, pkgs []Package) Package {
	t.Helper()
	if len(pkgs) != 1 {
		t.Fatalf("packages = %v, want exactly one", names(pkgs))
	}
	return pkgs[0]
}

// ── normalization ────────────────────────────────────────────────────────────

// Each rule here comes from the ecosystem's own case semantics. Getting one
// wrong means the index silently fails to join, so every rule gets a case.
func TestNormalize(t *testing.T) {
	tests := []struct {
		name      string
		ecosystem string
		in        string
		want      string
	}{
		// Go module paths are case-sensitive except the host, and
		// github.com/BurntSushi/toml is a real module whose capitals matter.
		{name: "go keeps path case, lowers host", ecosystem: Go, in: "GitHub.com/BurntSushi/toml", want: "github.com/BurntSushi/toml"},
		{name: "go bare module", ecosystem: Go, in: "Example", want: "example"},
		{name: "npm lowercases", ecosystem: NPM, in: "@Org/Shared", want: "@org/shared"},
		{name: "maven is case sensitive", ecosystem: Maven, in: "com.Org:Shared", want: "com.Org:Shared"},
		// PEP 503: lowercase, and both `_` and `.` fold to `-`.
		{name: "python folds separators", ecosystem: Python, in: "My_Lib.Core", want: "my-lib-core"},
		{name: "cargo folds underscore", ecosystem: Cargo, in: "My_Crate", want: "my-crate"},
		{name: "nuget lowercases", ecosystem: NuGet, in: "Org.Shared", want: "org.shared"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.ecosystem, tt.in); got != tt.want {
				t.Errorf("Normalize(%q, %q) = %q, want %q", tt.ecosystem, tt.in, got, tt.want)
			}
		})
	}
}

func TestPURL(t *testing.T) {
	tests := []struct {
		name      string
		ecosystem string
		pkg       string
		version   string
		want      string
	}{
		{name: "go", ecosystem: Go, pkg: "github.com/org/shared", version: "v1.2.0", want: "pkg:golang/github.com/org/shared@v1.2.0"},
		{name: "scoped npm", ecosystem: NPM, pkg: "@org/shared", version: "1.0.0", want: "pkg:npm/@org/shared@1.0.0"},
		{name: "unscoped npm", ecosystem: NPM, pkg: "shared", want: "pkg:npm/shared"},
		{name: "maven", ecosystem: Maven, pkg: "com.org:shared", version: "2.1", want: "pkg:maven/com.org/shared@2.1"},
		{name: "python", ecosystem: Python, pkg: "my-lib", want: "pkg:pypi/my-lib"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PURL(tt.ecosystem, tt.pkg, tt.version); got != tt.want {
				t.Errorf("PURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── go.mod ───────────────────────────────────────────────────────────────────

func TestParseGoMod(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantModule   string
		wantRequires []string
	}{
		{
			name: "block require",
			content: `module github.com/org/checkout

go 1.25

require (
	github.com/org/shared v1.2.0
	github.com/gin-gonic/gin v1.12.0
)
`,
			wantModule:   "github.com/org/checkout",
			wantRequires: []string{"github.com/org/shared", "github.com/gin-gonic/gin"},
		},
		{
			name: "single line require",
			content: `module github.com/org/checkout

require github.com/org/shared v1.2.0
`,
			wantModule:   "github.com/org/checkout",
			wantRequires: []string{"github.com/org/shared"},
		},
		{
			name: "indirect requirements still count as declared",
			content: `module github.com/org/checkout

require (
	github.com/org/shared v1.2.0 // indirect
)
`,
			wantModule:   "github.com/org/checkout",
			wantRequires: []string{"github.com/org/shared"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := ParseGoMod("go.mod", []byte(tt.content))
			if err != nil {
				t.Fatalf("ParseGoMod() error = %v, want nil", err)
			}
			if got := single(t, manifest.Published); got.Name != tt.wantModule {
				t.Errorf("published = %q, want %q", got.Name, tt.wantModule)
			}
			if len(manifest.Requires) != len(tt.wantRequires) {
				t.Fatalf("requires = %v, want %v", names(manifest.Requires), tt.wantRequires)
			}
			for _, want := range tt.wantRequires {
				if !hasName(manifest.Requires, want) {
					t.Errorf("requires = %v, want it to contain %q", names(manifest.Requires), want)
				}
			}
		})
	}
}

// A replace directive points a module at a fork or a local path for build
// purposes. Reading it as a declared dependency would put `../shared` — or a
// fork's path — into the index as if the repository consumed it.
func TestParseGoMod_IgnoresReplace(t *testing.T) {
	content := `module github.com/org/checkout

require github.com/org/shared v1.2.0

replace github.com/org/shared => ../shared
`
	manifest, err := ParseGoMod("go.mod", []byte(content))
	if err != nil {
		t.Fatalf("ParseGoMod() error = %v, want nil", err)
	}
	if got := names(manifest.Requires); len(got) != 1 || got[0] != "github.com/org/shared" {
		t.Errorf("requires = %v, want only the required module", got)
	}
}

// A malformed manifest is an error the caller turns into an incomplete fact. It
// must never panic — a repository with a broken go.mod would otherwise take the
// worker down.
func TestParseGoMod_MalformedIsAnErrorNotAPanic(t *testing.T) {
	if _, err := ParseGoMod("go.mod", []byte("module\x00 ???\nrequire (((")); err == nil {
		t.Error("ParseGoMod() error = nil, want a parse error")
	}
}

// ── package.json ─────────────────────────────────────────────────────────────

func TestParseNPMPackage_AllThreeDependencyMaps(t *testing.T) {
	// devDependencies count: an internal eslint config or test helper is
	// declared there, and dropping them hides a real internal edge.
	content := `{
	  "name": "@org/checkout",
	  "dependencies": {"@org/shared": "^1.2.0"},
	  "devDependencies": {"@org/eslint-config": "1.0.0"},
	  "peerDependencies": {"react": "^18"}
	}`
	manifest, err := ParseNPMPackage("package.json", []byte(content))
	if err != nil {
		t.Fatalf("ParseNPMPackage() error = %v, want nil", err)
	}
	if got := single(t, manifest.Published); got.Name != "@org/checkout" {
		t.Errorf("published = %q, want %q", got.Name, "@org/checkout")
	}
	for _, want := range []string{"@org/shared", "@org/eslint-config", "react"} {
		if !hasName(manifest.Requires, want) {
			t.Errorf("requires = %v, want it to contain %q", names(manifest.Requires), want)
		}
	}
}

// A private application root legitimately has no name. It publishes nothing,
// which is not an error — treating it as one would fail the whole extraction.
func TestParseNPMPackage_NoNamePublishesNothing(t *testing.T) {
	manifest, err := ParseNPMPackage("package.json", []byte(`{"private": true, "dependencies": {"@org/shared": "1.0.0"}}`))
	if err != nil {
		t.Fatalf("ParseNPMPackage() error = %v, want nil", err)
	}
	if len(manifest.Published) != 0 {
		t.Errorf("published = %v, want none", names(manifest.Published))
	}
	if !hasName(manifest.Requires, "@org/shared") {
		t.Errorf("requires = %v, want the dependency to survive", names(manifest.Requires))
	}
}

// A workspace root is `private: true` with no name in almost every monorepo: it
// publishes nothing, and the caller finds its sub-manifests by walking the tree
// rather than by following the globs declared here.
func TestParseNPMPackage_WorkspaceRootPublishesNothing(t *testing.T) {
	manifest, err := ParseNPMPackage("package.json", []byte(`{"private": true, "workspaces": ["packages/*"]}`))
	if err != nil {
		t.Fatalf("ParseNPMPackage() error = %v, want nil", err)
	}
	if len(manifest.Published) != 0 {
		t.Errorf("published = %v, want none", names(manifest.Published))
	}
}

// npm accepts three shapes for `repository`. A typed field would fail to decode
// two of them and take the rest of the manifest down with it.
func TestParseNPMPackage_RepositoryShapes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "object", content: `{"name":"x","repository":{"type":"git","url":"git+https://github.com/org/x.git"}}`, want: "git+https://github.com/org/x.git"},
		{name: "string", content: `{"name":"x","repository":"https://github.com/org/x"}`, want: "https://github.com/org/x"},
		{name: "shorthand", content: `{"name":"x","repository":"org/x"}`, want: "org/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := ParseNPMPackage("package.json", []byte(tt.content))
			if err != nil {
				t.Fatalf("ParseNPMPackage() error = %v, want nil", err)
			}
			if manifest.RepositoryURL != tt.want {
				t.Errorf("repository = %q, want %q", manifest.RepositoryURL, tt.want)
			}
		})
	}
}

func TestParseNPMPackage_MalformedIsAnErrorNotAPanic(t *testing.T) {
	if _, err := ParseNPMPackage("package.json", []byte(`{"name": `)); err == nil {
		t.Error("ParseNPMPackage() error = nil, want a parse error")
	}
}

// ── pom.xml ──────────────────────────────────────────────────────────────────

func TestParseMavenPOM(t *testing.T) {
	content := `<project>
	  <groupId>com.org</groupId>
	  <artifactId>checkout</artifactId>
	  <scm><url>https://github.com/org/checkout</url></scm>
	  <dependencies>
	    <dependency><groupId>com.org</groupId><artifactId>shared</artifactId><version>1.2.0</version></dependency>
	  </dependencies>
	</project>`
	manifest, err := ParseMavenPOM("pom.xml", []byte(content))
	if err != nil {
		t.Fatalf("ParseMavenPOM() error = %v, want nil", err)
	}
	if got := single(t, manifest.Published); got.Name != "com.org:checkout" {
		t.Errorf("published = %q, want %q", got.Name, "com.org:checkout")
	}
	if got := single(t, manifest.Requires); got.Name != "com.org:shared" {
		t.Errorf("requires = %q, want %q", got.Name, "com.org:shared")
	}
	if manifest.RepositoryURL != "https://github.com/org/checkout" {
		t.Errorf("repository = %q, want the scm url", manifest.RepositoryURL)
	}
}

// A `groupId` inherited from the parent POM is legal and common in a
// multi-module build, so the coordinate has to fall back to it.
func TestParseMavenPOM_InheritsGroupIDFromParent(t *testing.T) {
	content := `<project>
	  <parent><groupId>com.org</groupId><artifactId>platform</artifactId></parent>
	  <artifactId>checkout</artifactId>
	</project>`
	manifest, err := ParseMavenPOM("pom.xml", []byte(content))
	if err != nil {
		t.Fatalf("ParseMavenPOM() error = %v, want nil", err)
	}
	if got := single(t, manifest.Published); got.Name != "com.org:checkout" {
		t.Errorf("published = %q, want %q", got.Name, "com.org:checkout")
	}
}

// A property placeholder is irresolvable in a shallow read. Refusing costs a
// possible edge; guessing invents one at confidence 1.00, which is worse than
// having no edge at all.
func TestParseMavenPOM_PropertyPlaceholderYieldsNoCoordinate(t *testing.T) {
	content := `<project>
	  <groupId>com.org</groupId>
	  <artifactId>${revision}</artifactId>
	  <dependencies>
	    <dependency><groupId>${shared.group}</groupId><artifactId>shared</artifactId></dependency>
	  </dependencies>
	</project>`
	manifest, err := ParseMavenPOM("pom.xml", []byte(content))
	if err != nil {
		t.Fatalf("ParseMavenPOM() error = %v, want nil", err)
	}
	if len(manifest.Published) != 0 {
		t.Errorf("published = %v, want none", names(manifest.Published))
	}
	if len(manifest.Requires) != 0 {
		t.Errorf("requires = %v, want none", names(manifest.Requires))
	}
}

// ── python ───────────────────────────────────────────────────────────────────

func TestParsePyProject_PEP621(t *testing.T) {
	content := `[project]
name = "My_Checkout"
version = "1.0.0"
dependencies = ["org-shared>=1.2", "requests"]

[project.urls]
Repository = "https://github.com/org/checkout"
`
	manifest, err := ParsePyProject("pyproject.toml", []byte(content))
	if err != nil {
		t.Fatalf("ParsePyProject() error = %v, want nil", err)
	}
	if got := single(t, manifest.Published); got.Name != "my-checkout" {
		t.Errorf("published = %q, want %q", got.Name, "my-checkout")
	}
	if !hasName(manifest.Requires, "org-shared") || !hasName(manifest.Requires, "requests") {
		t.Errorf("requires = %v, want org-shared and requests", names(manifest.Requires))
	}
	if manifest.RepositoryURL != "https://github.com/org/checkout" {
		t.Errorf("repository = %q, want the project url", manifest.RepositoryURL)
	}
}

// Poetry's layout coexists with PEP 621 in the wild, and `python` in its
// dependency table is the interpreter constraint, not a package.
func TestParsePyProject_PoetryLayoutDropsPythonConstraint(t *testing.T) {
	content := `[tool.poetry]
name = "checkout"
repository = "https://github.com/org/checkout"

[tool.poetry.dependencies]
python = "^3.11"
org-shared = "^1.2"
`
	manifest, err := ParsePyProject("pyproject.toml", []byte(content))
	if err != nil {
		t.Fatalf("ParsePyProject() error = %v, want nil", err)
	}
	if got := single(t, manifest.Published); got.Name != "checkout" {
		t.Errorf("published = %q, want %q", got.Name, "checkout")
	}
	if got := single(t, manifest.Requires); got.Name != "org-shared" {
		t.Errorf("requires = %q, want only org-shared", got.Name)
	}
}

func TestParseRequirements(t *testing.T) {
	// `-r` includes another file and `-e` is a local editable checkout. Reading
	// either as a name puts a file path into the package index.
	content := `# production pins
-r base.txt
--index-url https://pypi.internal/simple
-e ./libs/local

org-shared[async]>=1.0,<2.0
Requests == 2.31.0  # pinned for the retry fix
flask ; python_version < "3.12"
`
	manifest, err := ParseRequirements("requirements.txt", []byte(content))
	if err != nil {
		t.Fatalf("ParseRequirements() error = %v, want nil", err)
	}
	want := []string{"org-shared", "requests", "flask"}
	if len(manifest.Requires) != len(want) {
		t.Fatalf("requires = %v, want %v", names(manifest.Requires), want)
	}
	for _, name := range want {
		if !hasName(manifest.Requires, name) {
			t.Errorf("requires = %v, want it to contain %q", names(manifest.Requires), name)
		}
	}
	// A consumption list publishes nothing by definition.
	if len(manifest.Published) != 0 {
		t.Errorf("published = %v, want none", names(manifest.Published))
	}
}

// ── Cargo.toml ───────────────────────────────────────────────────────────────

func TestParseCargoToml(t *testing.T) {
	// Dependency values are a version string *or* an inline table; a typed
	// map[string]string would fail to decode the whole file on the second form.
	content := `[package]
name = "checkout_service"
repository = "https://github.com/org/checkout"

[dependencies]
org-shared = "1.2"
serde = { version = "1.0", features = ["derive"] }

[dev-dependencies]
org_test_helpers = "0.1"
`
	manifest, err := ParseCargoToml("Cargo.toml", []byte(content))
	if err != nil {
		t.Fatalf("ParseCargoToml() error = %v, want nil", err)
	}
	if got := single(t, manifest.Published); got.Name != "checkout-service" {
		t.Errorf("published = %q, want %q", got.Name, "checkout-service")
	}
	for _, want := range []string{"org-shared", "serde", "org-test-helpers"} {
		if !hasName(manifest.Requires, want) {
			t.Errorf("requires = %v, want it to contain %q", names(manifest.Requires), want)
		}
	}
}

// A workspace root has [workspace] and no [package]: it publishes nothing, and
// its member crates are found by walking the tree.
func TestParseCargoToml_WorkspaceRootPublishesNothing(t *testing.T) {
	manifest, err := ParseCargoToml("Cargo.toml", []byte("[workspace]\nmembers = [\"crates/*\"]\n"))
	if err != nil {
		t.Fatalf("ParseCargoToml() error = %v, want nil", err)
	}
	if len(manifest.Published) != 0 {
		t.Errorf("published = %v, want none", names(manifest.Published))
	}
}

// ── .csproj ──────────────────────────────────────────────────────────────────

func TestParseCsproj(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		want    string
	}{
		{
			name:    "PackageId wins",
			path:    "src/Checkout/Checkout.csproj",
			content: `<Project><PropertyGroup><PackageId>Org.Checkout</PackageId><AssemblyName>Other</AssemblyName></PropertyGroup></Project>`,
			want:    "org.checkout",
		},
		{
			name:    "AssemblyName is the fallback",
			path:    "src/Checkout/Checkout.csproj",
			content: `<Project><PropertyGroup><AssemblyName>Org.Checkout</AssemblyName></PropertyGroup></Project>`,
			want:    "org.checkout",
		},
		{
			// What MSBuild itself does when neither property is set, which is
			// the overwhelming majority of projects.
			name:    "file base name is the last resort",
			path:    "src/Checkout/Org.Checkout.csproj",
			content: `<Project><PropertyGroup /></Project>`,
			want:    "org.checkout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := ParseCsproj(tt.path, []byte(tt.content))
			if err != nil {
				t.Fatalf("ParseCsproj() error = %v, want nil", err)
			}
			if got := single(t, manifest.Published); got.Name != tt.want {
				t.Errorf("published = %q, want %q", got.Name, tt.want)
			}
		})
	}
}

func TestParseCsproj_PackageReferences(t *testing.T) {
	content := `<Project>
	  <PropertyGroup><PackageId>Org.Checkout</PackageId><RepositoryUrl>https://github.com/org/checkout</RepositoryUrl></PropertyGroup>
	  <ItemGroup>
	    <PackageReference Include="Org.Shared" Version="1.2.0" />
	    <PackageReference Include="$(SharedPackage)" Version="1.0.0" />
	  </ItemGroup>
	</Project>`
	manifest, err := ParseCsproj("Checkout.csproj", []byte(content))
	if err != nil {
		t.Fatalf("ParseCsproj() error = %v, want nil", err)
	}
	// MSBuild property syntax is NuGet's `${revision}`: unresolvable, so refused.
	if got := single(t, manifest.Requires); got.Name != "org.shared" {
		t.Errorf("requires = %q, want only org.shared", got.Name)
	}
	if manifest.RepositoryURL != "https://github.com/org/checkout" {
		t.Errorf("repository = %q, want the RepositoryUrl", manifest.RepositoryURL)
	}
}

// ── dispatch ─────────────────────────────────────────────────────────────────

func TestParserFor(t *testing.T) {
	tests := []struct {
		path       string
		isManifest bool
	}{
		{path: "go.mod", isManifest: true},
		{path: "services/api/go.mod", isManifest: true},
		{path: "package.json", isManifest: true},
		{path: "pom.xml", isManifest: true},
		{path: "pyproject.toml", isManifest: true},
		{path: "Cargo.toml", isManifest: true},
		{path: "requirements.txt", isManifest: true},
		{path: "requirements-dev.txt", isManifest: true},
		{path: "requirements/base.txt", isManifest: true},
		{path: "src/App/App.csproj", isManifest: true},
		{path: "go.sum", isManifest: false},
		{path: "package-lock.json", isManifest: false},
		{path: "Cargo.lock", isManifest: false},
		{path: "README.md", isManifest: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsManifest(tt.path); got != tt.isManifest {
				t.Errorf("IsManifest(%q) = %v, want %v", tt.path, got, tt.isManifest)
			}
		})
	}
}
