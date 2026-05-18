package docs

import "testing"

func TestTemplates_ContainsExpectedIDs(t *testing.T) {
	expected := map[string]TemplateType{
		TemplateIDADRTechChoice:  TemplateTypeADR,
		TemplateIDADRBoundary:    TemplateTypeADR,
		TemplateIDADRDeprecation: TemplateTypeADR,
		TemplateIDADRConvention:  TemplateTypeADR,
		TemplateIDArchitecture:   TemplateTypeArchitecture,
		TemplateIDGuidelines:     TemplateTypeGuidelines,
	}

	templates := Templates()
	if len(templates) != len(expected) {
		t.Fatalf("Templates() length = %d, want %d", len(templates), len(expected))
	}

	for _, tmpl := range templates {
		wantType, ok := expected[tmpl.ID]
		if !ok {
			t.Errorf("unexpected template id: %s", tmpl.ID)
			continue
		}
		if tmpl.Type != wantType {
			t.Errorf("template %s type = %s, want %s", tmpl.ID, tmpl.Type, wantType)
		}
		if tmpl.Scope != TemplateScopeOrg {
			t.Errorf("template %s scope = %s, want %s", tmpl.ID, tmpl.Scope, TemplateScopeOrg)
		}
		if len(tmpl.Sections) == 0 {
			t.Errorf("template %s should expose preview sections", tmpl.ID)
		}
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
