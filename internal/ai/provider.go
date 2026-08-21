package ai

import "context"

type DocumentationType string

const (
	DocumentationTypeADR          DocumentationType = "adr"
	DocumentationTypeArchitecture DocumentationType = "architecture"
	DocumentationTypeServiceDoc   DocumentationType = "service_doc"
	DocumentationTypeGuidelines   DocumentationType = "guidelines"
)

type DocumentationRequest struct {
	Type            DocumentationType
	RepositoryID    string
	RepoName        string
	Branch          string
	Languages       []string
	Frameworks      []string
	Topics          []string
	ContextMarkdown string
	// OutputLanguage is the BCP 47 tag for the Markdown body. Empty or "en"
	// produces English output.
	OutputLanguage string
}

// OrgDocumentationRequest carries the inputs for org-wide doc generation
// (ADRs, Architecture overview, Engineering Guidelines).
//
// `OrgContextMarkdown` is the aggregated org snapshot (repos, dominant
// stacks, relationships, existing per-repo docs) produced by the
// OrgContextBuilder. `TemplateID` is required when `Type` is `adr` — it
// selects which ADR template (Technology Choice, Service Boundary,
// Deprecation, Cross-cutting Convention) to render. `UserPrompt` is the
// free-text topic the user typed in the modal (e.g. "Should we standardize
// on PostgreSQL?"); empty for architecture / guidelines.
type OrgDocumentationRequest struct {
	Type               DocumentationType
	OrganizationID     string
	OrganizationName   string
	OrgContextMarkdown string
	TemplateID         string
	UserPrompt         string
	OutputLanguage     string
}

type DocumentationResult struct {
	Content    string
	Model      string
	TokensUsed int
}

// DocumentationGenerator is the extension point for pluggable LLM providers.
// Documentation generation is the only AI-backed feature the platform ships.
type DocumentationGenerator interface {
	GenerateDocumentation(ctx context.Context, req *DocumentationRequest) (*DocumentationResult, error)
	// GenerateOrgDocumentation produces an org-wide document (ADR /
	// architecture overview / engineering guidelines) using aggregated
	// organization context rather than per-repo clones.
	GenerateOrgDocumentation(ctx context.Context, req *OrgDocumentationRequest) (*DocumentationResult, error)
	Provider() string
}
