package tasks

// Task type constants used by both the enqueuer and the worker handler registry.
// Add new constants here as features are implemented.
const (
	TypeSyncRepo       = "repo:sync"
	TypeProcessWebhook = "webhook:process"
	TypeGenerateDocs   = "docs:generate"
	// TypeDeriveArchitecture reconciles derived architecture for one
	// organization. It is org-scoped and not repo-scoped because an internal
	// edge is inherently org-wide: knowing that A depends on B needs the index
	// of names published by every repository in the organization.
	TypeDeriveArchitecture = "architecture:derive"
)

type SyncRepoPayload struct {
	RepositoryID string `json:"repository_id"`
}

type DeriveArchitecturePayload struct {
	OrganizationID string `json:"organization_id"`
}

type WebhookProcessPayload struct {
	WebhookID string `json:"webhook_id"`
}

type GenerateDocsPayload struct {
	DocGenerationID string   `json:"doc_generation_id"`
	RepositoryID    string   `json:"repository_id"`
	Types           []string `json:"types"`
	Branch          string   `json:"branch,omitempty"`
	TriggeredByID   string   `json:"triggered_by_id,omitempty"`
}

// GenerateOrgDocsPayload carries the inputs for org-wide doc generation.
// `DocGenerationID` is the pre-created row (status `pending`) that the
// worker should populate. `TemplateID` is required only for ADR rows.
type GenerateOrgDocsPayload struct {
	DocGenerationID string   `json:"doc_generation_id"`
	OrganizationID  string   `json:"organization_id"`
	Types           []string `json:"types"`
	TemplateID      string   `json:"template_id,omitempty"`
	UserPrompt      string   `json:"user_prompt,omitempty"`
	TriggeredByID   string   `json:"triggered_by_id,omitempty"`
}

const TypeGenerateOrgDocs = "docs:generate_org"
