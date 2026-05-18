package anthropic

import (
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/docs"
)

// orgPromptFor invokes the (unexported) prompt builder via a zero-value
// client. The builder doesn't touch the SDK client, so we don't need a
// real Anthropic HTTP transport here.
func orgPromptFor(req *ai.OrgDocumentationRequest) (string, error) {
	c := &Client{}
	return c.buildOrgDocumentationPrompt(req)
}

func TestBuildOrgDocumentationPrompt_RejectsRepoScopeTypes(t *testing.T) {
	_, err := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeServiceDoc,
		OrgContextMarkdown: "# Org",
	})
	if err == nil {
		t.Fatal("expected error for non-org documentation type")
	}
}

func TestBuildOrgDocumentationPrompt_ADRTechChoiceHasMADRSections(t *testing.T) {
	prompt, err := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeADR,
		TemplateID:         docs.TemplateIDADRTechChoice,
		UserPrompt:         "Postgres vs Mongo for the auth service",
		OrgContextMarkdown: "# Organization: Acme\n",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MADR signature sections must be present so Claude follows the format.
	for _, want := range []string{
		"Decision Drivers",
		"Considered Options",
		"Pros and Cons",
		"Consequences",
		"Postgres vs Mongo for the auth service", // user prompt echoed verbatim
		"ORGANIZATION CONTEXT:",                   // header preserved
		"# Organization: Acme",                    // context body included
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("ADR tech-choice prompt missing %q\nprompt:\n%s", want, prompt)
		}
	}
}

func TestBuildOrgDocumentationPrompt_ADRBoundaryReferencesGraph(t *testing.T) {
	prompt, _ := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeADR,
		TemplateID:         docs.TemplateIDADRBoundary,
		OrgContextMarkdown: "# Org",
	})
	if !strings.Contains(prompt, "Relationships Impacted") {
		t.Errorf("boundary prompt should reference graph relationships:\n%s", prompt)
	}
}

func TestBuildOrgDocumentationPrompt_ADRDeprecationIncludesTimeline(t *testing.T) {
	prompt, _ := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeADR,
		TemplateID:         docs.TemplateIDADRDeprecation,
		OrgContextMarkdown: "# Org",
	})
	for _, want := range []string{"Timeline", "Migration Path"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("deprecation prompt missing %q", want)
		}
	}
}

func TestBuildOrgDocumentationPrompt_ADRConventionIsYStatement(t *testing.T) {
	prompt, _ := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeADR,
		TemplateID:         docs.TemplateIDADRConvention,
		OrgContextMarkdown: "# Org",
	})
	if !strings.Contains(prompt, "In the context of") {
		t.Errorf("convention prompt should embed the Y-statement literal:\n%s", prompt)
	}
}

func TestBuildOrgDocumentationPrompt_ArchitectureMentionsMermaid(t *testing.T) {
	prompt, _ := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeArchitecture,
		OrgContextMarkdown: "# Org",
	})
	for _, want := range []string{
		"Visão geral",
		"Mermaid",
		"Padrões transversais",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("architecture prompt missing %q", want)
		}
	}
}

func TestBuildOrgDocumentationPrompt_GuidelinesCoversSixSections(t *testing.T) {
	prompt, _ := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeGuidelines,
		OrgContextMarkdown: "# Org",
	})
	for _, want := range []string{
		"Processo de PR",
		"Code style",
		"Convenções de nomenclatura",
		"Segurança",
		"Testes",
		"Observabilidade",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("guidelines prompt missing %q", want)
		}
	}
}

func TestBuildOrgDocumentationPrompt_EscapesUserTopic(t *testing.T) {
	prompt, _ := orgPromptFor(&ai.OrgDocumentationRequest{
		Type:               ai.DocumentationTypeADR,
		TemplateID:         docs.TemplateIDADRTechChoice,
		UserPrompt:         `<script>alert("x")</script>`,
		OrgContextMarkdown: "# Org",
	})
	if strings.Contains(prompt, "<script>") {
		t.Errorf("user topic must be HTML-escaped to limit prompt injection:\n%s", prompt)
	}
	if !strings.Contains(prompt, "&lt;script&gt;") {
		t.Errorf("escaped user topic should still appear (just sanitised):\n%s", prompt)
	}
}
