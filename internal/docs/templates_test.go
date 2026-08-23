package docs

import (
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/ai"
)

func TestTemplates_ContainsExpectedIDs(t *testing.T) {
	expected := map[string]struct {
		docType TemplateType
		scope   TemplateScope
	}{
		TemplateIDADRTechChoice:    {TemplateTypeADR, TemplateScopeOrg},
		TemplateIDADRBoundary:      {TemplateTypeADR, TemplateScopeOrg},
		TemplateIDADRDeprecation:   {TemplateTypeADR, TemplateScopeOrg},
		TemplateIDADRConvention:    {TemplateTypeADR, TemplateScopeOrg},
		TemplateIDArchitecture:     {TemplateTypeArchitecture, TemplateScopeOrg},
		TemplateIDGuidelines:       {TemplateTypeGuidelines, TemplateScopeOrg},
		TemplateIDRepoArchitecture: {TemplateTypeArchitecture, TemplateScopeRepo},
		TemplateIDRepoServiceDoc:   {TemplateTypeServiceDoc, TemplateScopeRepo},
		TemplateIDRepoADR:          {TemplateTypeADR, TemplateScopeRepo},
		TemplateIDRepoGuidelines:   {TemplateTypeGuidelines, TemplateScopeRepo},
	}

	templates := Templates()
	if len(templates) != len(expected) {
		t.Fatalf("Templates() length = %d, want %d", len(templates), len(expected))
	}

	for _, tmpl := range templates {
		want, ok := expected[tmpl.ID]
		if !ok {
			t.Errorf("unexpected template id: %s", tmpl.ID)
			continue
		}
		if tmpl.Type != want.docType {
			t.Errorf("template %s type = %s, want %s", tmpl.ID, tmpl.Type, want.docType)
		}
		if tmpl.Scope != want.scope {
			t.Errorf("template %s scope = %s, want %s", tmpl.ID, tmpl.Scope, want.scope)
		}
	}
}

// Every entry has to answer "what comes out of this?". An entry with no
// description or no sections is the state the repo modal was in before this
// registry covered it, and is the thing worth preventing from coming back.
func TestTemplates_EveryEntryExplainsItself(t *testing.T) {
	for _, tmpl := range Templates() {
		if strings.TrimSpace(tmpl.Label) == "" {
			t.Errorf("template %s has no label", tmpl.ID)
		}
		if strings.TrimSpace(tmpl.Description) == "" {
			t.Errorf("template %s has no description", tmpl.ID)
		}
		if len(tmpl.Sections) == 0 {
			t.Errorf("template %s exposes no preview sections", tmpl.ID)
		}
	}
}

func TestTemplates_IDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, tmpl := range Templates() {
		if seen[tmpl.ID] {
			t.Errorf("duplicate template id: %s", tmpl.ID)
		}
		seen[tmpl.ID] = true
	}
}

// OutputPath is what makes the promise "this produces docs/ARCHITECTURE.md"
// true, and it only means something for a scope that has a repository.
func TestTemplates_OutputPathBelongsToRepoScopeOnly(t *testing.T) {
	for _, tmpl := range Templates() {
		switch tmpl.Scope {
		case TemplateScopeRepo:
			if tmpl.OutputPath == "" {
				t.Errorf("repo template %s must name the file it produces", tmpl.ID)
			}
		case TemplateScopeOrg:
			if tmpl.OutputPath != "" {
				t.Errorf("org template %s has OutputPath %q, but there is no repository to commit to", tmpl.ID, tmpl.OutputPath)
			}
		}
	}
}

func TestTemplatesForScope_DoesNotMixScopes(t *testing.T) {
	org := TemplatesForScope(TemplateScopeOrg)
	if len(org) == 0 {
		t.Fatal("TemplatesForScope(org) returned nothing")
	}
	for _, tmpl := range org {
		if tmpl.Scope != TemplateScopeOrg {
			t.Errorf("scope=org leaked %s (%s) — the org gallery would offer a repository file", tmpl.ID, tmpl.Scope)
		}
	}

	repo := TemplatesForScope(TemplateScopeRepo)
	if len(repo) == 0 {
		t.Fatal("TemplatesForScope(repo) returned nothing")
	}
	for _, tmpl := range repo {
		if tmpl.Scope != TemplateScopeRepo {
			t.Errorf("scope=repo leaked %s (%s)", tmpl.ID, tmpl.Scope)
		}
	}

	if len(org)+len(repo) != len(Templates()) {
		t.Errorf("the two scopes (%d + %d) do not add up to Templates() (%d)", len(org), len(repo), len(Templates()))
	}
}

func TestTemplatesForScope_UnknownScopeReturnsNothing(t *testing.T) {
	if got := TemplatesForScope("everything"); got != nil {
		t.Errorf("TemplatesForScope(unknown) = %v, want nil", got)
	}
}

// The registry has to cover every type the generator can be asked for, or the
// modal would offer a type whose output path nothing can resolve.
func TestPathForType_CoversEveryGeneratableType(t *testing.T) {
	tests := []struct {
		docType ai.DocumentationType
		want    string
	}{
		{ai.DocumentationTypeADR, "docs/adr/README.md"},
		{ai.DocumentationTypeArchitecture, "docs/ARCHITECTURE.md"},
		{ai.DocumentationTypeServiceDoc, "docs/SERVICE.md"},
		{ai.DocumentationTypeGuidelines, "CONTRIBUTING.md"},
	}

	for _, tt := range tests {
		t.Run(string(tt.docType), func(t *testing.T) {
			got, ok := PathForType(string(tt.docType))
			if !ok {
				t.Fatalf("PathForType(%s) not found", tt.docType)
			}
			// Pinned to the literal path: this is the file people's
			// repositories receive, so changing it is a breaking change and
			// should require editing this test on purpose.
			if got != tt.want {
				t.Errorf("PathForType(%s) = %q, want %q", tt.docType, got, tt.want)
			}
		})
	}
}

func TestPathForType_UnknownType(t *testing.T) {
	if _, ok := PathForType("runbook"); ok {
		t.Error("PathForType returned a path for a type the generator cannot produce")
	}
}

func TestGetTemplate_FoundAndMissing(t *testing.T) {
	tmpl, ok := GetTemplate(TemplateIDADRTechChoice)
	if !ok {
		t.Fatal("expected to find tech-choice template")
	}
	if tmpl.Type != TemplateTypeADR {
		t.Errorf("type = %s, want %s", tmpl.Type, TemplateTypeADR)
	}

	if _, ok := GetTemplate("nonexistent"); ok {
		t.Error("expected nonexistent template to be missing")
	}
}
