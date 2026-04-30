package ai

import "context"

type MockGenerator struct {
	GenerateTemplateFunc func(ctx context.Context, req *TemplateRequest) (*TemplateResult, error)
	ProviderFunc         func() string
}

func (m *MockGenerator) GenerateTemplate(ctx context.Context, req *TemplateRequest) (*TemplateResult, error) {
	if m.GenerateTemplateFunc != nil {
		return m.GenerateTemplateFunc(ctx, req)
	}
	return &TemplateResult{
		Summary: "Mock template",
		Files: []GeneratedFile{
			{Path: "README.md", Content: "# Mock template\n", Language: "markdown"},
		},
		Model:      "mock",
		TokensUsed: 0,
	}, nil
}

func (m *MockGenerator) Provider() string {
	if m.ProviderFunc != nil {
		return m.ProviderFunc()
	}
	return "mock"
}
