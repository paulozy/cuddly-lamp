package ai

import (
	"context"
)

type MockDocumentationGenerator struct {
	GenerateDocumentationFunc    func(ctx context.Context, req *DocumentationRequest) (*DocumentationResult, error)
	GenerateOrgDocumentationFunc func(ctx context.Context, req *OrgDocumentationRequest) (*DocumentationResult, error)
	ProviderFunc                 func() string
}

func (m *MockDocumentationGenerator) GenerateDocumentation(ctx context.Context, req *DocumentationRequest) (*DocumentationResult, error) {
	if m.GenerateDocumentationFunc != nil {
		return m.GenerateDocumentationFunc(ctx, req)
	}
	return &DocumentationResult{
		Content:    "# Mock documentation\n",
		Model:      "mock",
		TokensUsed: 0,
	}, nil
}

func (m *MockDocumentationGenerator) GenerateOrgDocumentation(ctx context.Context, req *OrgDocumentationRequest) (*DocumentationResult, error) {
	if m.GenerateOrgDocumentationFunc != nil {
		return m.GenerateOrgDocumentationFunc(ctx, req)
	}
	return &DocumentationResult{
		Content:    "# Mock org documentation\n",
		Model:      "mock",
		TokensUsed: 0,
	}, nil
}

func (m *MockDocumentationGenerator) Provider() string {
	if m.ProviderFunc != nil {
		return m.ProviderFunc()
	}
	return "mock"
}
