package ai

import (
	"context"
)

// MockAnalyzer is a test mock that implements Analyzer
type MockAnalyzer struct {
	AnalyzeCodeFunc func(ctx context.Context, req *AnalysisRequest) (*AnalysisResult, error)
	ProviderFunc    func() string
}

func (m *MockAnalyzer) AnalyzeCode(ctx context.Context, req *AnalysisRequest) (*AnalysisResult, error) {
	if m.AnalyzeCodeFunc != nil {
		return m.AnalyzeCodeFunc(ctx, req)
	}
	return &AnalysisResult{
		Summary:    "Mock analysis",
		Issues:     []CodeIssue{},
		Metrics:    CodeMetrics{},
		Model:      "mock",
		TokensUsed: 0,
	}, nil
}

func (m *MockAnalyzer) Provider() string {
	if m.ProviderFunc != nil {
		return m.ProviderFunc()
	}
	return "mock"
}

type MockDocumentationGenerator struct {
	GenerateDocumentationFunc func(ctx context.Context, req *DocumentationRequest) (*DocumentationResult, error)
	ProviderFunc              func() string
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

func (m *MockDocumentationGenerator) Provider() string {
	if m.ProviderFunc != nil {
		return m.ProviderFunc()
	}
	return "mock"
}
