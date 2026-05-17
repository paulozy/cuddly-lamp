package models

import "time"

type GenerateDocsRequest struct {
	Types  []string `json:"types" binding:"required,min=1"`
	Branch string   `json:"branch,omitempty"`
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
	RepositoryID      string              `json:"repository_id"`
	Status            DocGenerationStatus `json:"status"`
	Types             []string            `json:"types"`
	Branch            string              `json:"branch,omitempty"`
	GenBranch         string              `json:"gen_branch,omitempty"`
	PullRequestURL    string              `json:"pull_request_url,omitempty"`
	PullRequestNumber int                 `json:"pull_request_number,omitempty"`
	TokensUsed        int                 `json:"tokens_used"`
	ErrorMessage      string              `json:"error_message,omitempty"`
	TriggeredByUserID string              `json:"triggered_by_user_id,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
}

type DocGenerationListResponse struct {
	Items []DocGenerationSummary `json:"items"`
	Total int                    `json:"total"`
}

// ToDocGenerationSummary projects a DocGeneration row into its lightweight
// summary form (without the `content` JSONB).
func ToDocGenerationSummary(doc *DocGeneration) DocGenerationSummary {
	return DocGenerationSummary{
		ID:                doc.ID,
		RepositoryID:      doc.RepositoryID,
		Status:            doc.Status,
		Types:             []string(doc.Types),
		Branch:            doc.Branch,
		GenBranch:         doc.GenBranch,
		PullRequestURL:    doc.PullRequestURL,
		PullRequestNumber: doc.PullRequestNumber,
		TokensUsed:        doc.TokensUsed,
		ErrorMessage:      doc.ErrorMessage,
		TriggeredByUserID: doc.TriggeredByUserID,
		CreatedAt:         doc.CreatedAt,
		UpdatedAt:         doc.UpdatedAt,
	}
}
