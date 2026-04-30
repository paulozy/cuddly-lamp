package models

type GenerateDocsRequest struct {
	Types  []string `json:"types" binding:"required,min=1"`
	Branch string   `json:"branch,omitempty"`
}

type DocGenerationAcceptedResponse struct {
	ID     string              `json:"id"`
	Status DocGenerationStatus `json:"status"`
}
