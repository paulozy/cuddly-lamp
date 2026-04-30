package ai

import "context"

type Generator interface {
	GenerateTemplate(ctx context.Context, req *TemplateRequest) (*TemplateResult, error)
	Provider() string
}

type StackProfile struct {
	PrimaryLanguage    string   `json:"primary_language"`
	SecondaryLanguages []string `json:"secondary_languages,omitempty"`
	Frameworks         []string `json:"frameworks,omitempty"`
	Topics             []string `json:"topics,omitempty"`
	HasCI              bool     `json:"has_ci"`
	HasTests           bool     `json:"has_tests"`
}

type TemplateRequest struct {
	Prompt         string
	OrganizationID string
	RepositoryID   string
	Stack          StackProfile
	StackHint      string
	TemplateID     string
}

type GeneratedFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Language string `json:"language"`
}

type TemplateResult struct {
	Summary    string
	Files      []GeneratedFile
	Model      string
	TokensUsed int
}
