package tasks

// Task type constants used by both the enqueuer and the worker handler registry.
// Add new constants here as features are implemented.
const (
	TypeSyncRepo           = "repo:sync"
	TypeAnalyzeRepo        = "repo:analyze"
	TypeProcessWebhook     = "webhook:process"
	TypeGenerateEmbeddings = "embeddings:generate"
	TypeScanDependencies   = "dependency:scan"
	TypeGenerateTemplate   = "template:generate"
)

type SyncRepoPayload struct {
	RepositoryID string `json:"repository_id"`
}

type WebhookProcessPayload struct {
	WebhookID string `json:"webhook_id"`
}

type AnalyzeRepoPayload struct {
	RepositoryID  string `json:"repository_id"`
	Branch        string `json:"branch,omitempty"`
	CommitSHA     string `json:"commit_sha,omitempty"`
	PullRequestID int64  `json:"pull_request_id,omitempty"`
	Type          string `json:"type,omitempty"`
	TriggeredBy   string `json:"triggered_by,omitempty"`
}

type GenerateEmbeddingsPayload struct {
	RepositoryID string `json:"repository_id"`
	AnalysisID   string `json:"analysis_id,omitempty"`
	Branch       string `json:"branch,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
}

type ScanDependenciesPayload struct {
	RepositoryID  string `json:"repository_id"`
	Branch        string `json:"branch,omitempty"`
	CommitSHA     string `json:"commit_sha,omitempty"`
	PullRequestID int    `json:"pull_request_id,omitempty"`
	TriggeredBy   string `json:"triggered_by"`
}

type GenerateTemplatePayload struct {
	TemplateID     string `json:"template_id"`
	OrganizationID string `json:"organization_id"`
	RepositoryID   string `json:"repository_id,omitempty"`
	Prompt         string `json:"prompt"`
	StackHint      string `json:"stack_hint,omitempty"`
	TriggeredByID  string `json:"triggered_by_id"`
}
