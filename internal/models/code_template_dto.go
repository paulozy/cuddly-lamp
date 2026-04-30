package models

type GenerateTemplateRequest struct {
	Prompt    string `json:"prompt" binding:"required"`
	StackHint string `json:"stack_hint,omitempty"`
}

type PinTemplateRequest struct {
	IsPinned bool   `json:"is_pinned"`
	Name     string `json:"name,omitempty"`
}

type TemplateAcceptedResponse struct {
	ID     string         `json:"id"`
	Status TemplateStatus `json:"status"`
}

type TemplateListResponse struct {
	Total     int64          `json:"total"`
	Templates []CodeTemplate `json:"templates"`
	Limit     int            `json:"limit"`
	Offset    int            `json:"offset"`
}
