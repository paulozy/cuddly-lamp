package dependencies

import "testing"

func TestParseGoMod(t *testing.T) {
	content := `module example.com/app

require github.com/gin-gonic/gin v1.10.0

require (
	golang.org/x/crypto v0.31.0
	golang.org/x/sys v0.28.0 // indirect
)
`
	got := ParseGoMod(content)
	if len(got) != 3 {
		t.Fatalf("ParseGoMod returned %d packages, want 3", len(got))
	}
	if got[0].Name != "github.com/gin-gonic/gin" || got[0].Version != "v1.10.0" || !got[0].IsDirect {
		t.Fatalf("first package = %+v", got[0])
	}
	if got[2].Name != "golang.org/x/sys" || got[2].IsDirect {
		t.Fatalf("indirect package = %+v", got[2])
	}
}

func TestParsePackageJSON(t *testing.T) {
	content := `{
		"dependencies": {"react": "^19.0.0"},
		"devDependencies": {"vite": "^6.0.0"}
	}`
	got := ParsePackageJSON(content)
	if len(got) != 2 {
		t.Fatalf("ParsePackageJSON returned %d packages, want 2", len(got))
	}
	byName := map[string]Package{}
	for _, pkg := range got {
		byName[pkg.Name] = pkg
	}
	if byName["react"].Ecosystem != "npm" || !byName["react"].IsDirect {
		t.Fatalf("react package = %+v", byName["react"])
	}
	if byName["vite"].IsDirect {
		t.Fatalf("vite package = %+v", byName["vite"])
	}
}

func TestParseRequirementsTxt(t *testing.T) {
	content := `
# comment
django==5.1.2
requests>=2.32.0 ; python_version > "3.10"
-r dev-requirements.txt
`
	got := ParseRequirementsTxt(content)
	if len(got) != 2 {
		t.Fatalf("ParseRequirementsTxt returned %d packages, want 2", len(got))
	}
	if got[0].Name != "django" || got[0].Version != "5.1.2" || got[0].Ecosystem != "pip" {
		t.Fatalf("first package = %+v", got[0])
	}
	if got[1].Name != "requests" || got[1].Version != "2.32.0" {
		t.Fatalf("second package = %+v", got[1])
	}
}

func TestParseCargoToml(t *testing.T) {
	content := `
[dependencies]
serde = "1.0"
tokio = { version = "1.42", features = ["full"] }
`
	got := ParseCargoToml(content)
	if len(got) != 2 {
		t.Fatalf("ParseCargoToml returned %d packages, want 2", len(got))
	}
	byName := map[string]Package{}
	for _, pkg := range got {
		byName[pkg.Name] = pkg
	}
	if byName["serde"].Version != "1.0" || byName["serde"].Ecosystem != "cargo" {
		t.Fatalf("serde package = %+v", byName["serde"])
	}
	if byName["tokio"].Version != "1.42" {
		t.Fatalf("tokio package = %+v", byName["tokio"])
	}
}
