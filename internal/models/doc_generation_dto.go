package models

import "time"

type GenerateDocsRequest struct {
	Types  []string `json:"types" binding:"required,min=1"`
	Branch string   `json:"branch,omitempty"`
}

// GenerateOrgDocsRequest is the body of POST /organizations/docs/generate.
// `Types` must be a non-empty subset of `adr`, `architecture`, `guidelines`.
// `TemplateID` is required only when the user asks for an ADR (so the worker
// knows which prompt to use). `Prompt` is the user-facing "topic" the user
// typed in the modal.
type GenerateOrgDocsRequest struct {
	Types      []string `json:"types" binding:"required,min=1"`
	TemplateID string   `json:"template_id,omitempty"`
	Prompt     string   `json:"prompt,omitempty"`
}

// UpdateDocContentRequest is the body of PATCH /docs/:id used to commit a
// manual edit. The new content map replaces the previous values for the
// types it contains; missing keys are inherited from the previous version.
type UpdateDocContentRequest struct {
	Content map[string]string `json:"content" binding:"required"`
}

// CreateManualDocRequest is the body of POST /repositories/:id/docs and
// POST /organizations/docs — a document written by a person.
//
// One type per request, unlike the generation flow which accepts several: a
// person writes one document at a time, and the batching in the generation
// path exists only to spend one pull request on several files.
type CreateManualDocRequest struct {
	Type    string `json:"type" binding:"required"`
	Content string `json:"content" binding:"required"`
}

type DocGenerationAcceptedResponse struct {
	ID     string              `json:"id"`
	Status DocGenerationStatus `json:"status"`
}

// DocGenerationSummary is the lightweight projection returned by listing
// endpoints. It intentionally omits the `content` JSONB blob (which can hold
// multiple kilobytes of Markdown per type) so the list response stays small.
type DocGenerationSummary struct {
	ID                string              `json:"id"`
	OrganizationID    string              `json:"organization_id"`
	Scope             DocGenerationScope  `json:"scope"`
	RepositoryID      string              `json:"repository_id,omitempty"`
	TemplateID        string              `json:"template_id,omitempty"`
	SupersededByID    string              `json:"superseded_by_id,omitempty"`
	Source            DocSource           `json:"source"`
	ProgressStage     string              `json:"progress_stage,omitempty"`
	Status            DocGenerationStatus `json:"status"`
	Types             []string            `json:"types"`
	Branch            string              `json:"branch,omitempty"`
	GenBranch         string              `json:"gen_branch,omitempty"`
	PullRequestURL    string              `json:"pull_request_url,omitempty"`
	PullRequestNumber int                 `json:"pull_request_number,omitempty"`
	TokensUsed        int                 `json:"tokens_used"`
	ErrorMessage      string              `json:"error_message,omitempty"`
	TriggeredByUserID string              `json:"triggered_by_user_id,omitempty"`
	UserPrompt        string              `json:"user_prompt,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
}

type DocGenerationListResponse struct {
	Items []DocGenerationSummary `json:"items"`
	Total int                    `json:"total"`
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ToDocGenerationSummary projects a DocGeneration row into its lightweight
// summary form (without the `content` JSONB).
func ToDocGenerationSummary(doc *DocGeneration) DocGenerationSummary {
	progress := ""
	if doc.ProgressStage != nil {
		progress = string(*doc.ProgressStage)
	}
	return DocGenerationSummary{
		ID:                doc.ID,
		OrganizationID:    doc.OrganizationID,
		Scope:             doc.Scope,
		RepositoryID:      derefString(doc.RepositoryID),
		TemplateID:        derefString(doc.TemplateID),
		SupersededByID:    derefString(doc.SupersededByID),
		Source:            doc.Source,
		ProgressStage:     progress,
		Status:            doc.Status,
		Types:             []string(doc.Types),
		Branch:            doc.Branch,
		GenBranch:         doc.GenBranch,
		PullRequestURL:    doc.PullRequestURL,
		PullRequestNumber: doc.PullRequestNumber,
		TokensUsed:        doc.TokensUsed,
		ErrorMessage:      doc.ErrorMessage,
		TriggeredByUserID: doc.TriggeredByUserID,
		UserPrompt:        doc.UserPrompt,
		CreatedAt:         doc.CreatedAt,
		UpdatedAt:         doc.UpdatedAt,
	}
}
