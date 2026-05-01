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

// MockSynthesizer is a test mock that implements Synthesizer. It supports two
// modes: a custom StreamFunc, or a scripted Script that is replayed verbatim.
// LastLanguage records the language passed to the most recent call so tests
// can assert that callers propagate the org's configured output language.
type MockSynthesizer struct {
	StreamFunc   func(ctx context.Context, query, language string, snippets []SearchSnippet) (<-chan SynthesisEvent, error)
	Script       []SynthesisEvent
	StartErr     error
	LastLanguage string
}

func (m *MockSynthesizer) StreamSearchSynthesis(ctx context.Context, query, language string, snippets []SearchSnippet) (<-chan SynthesisEvent, error) {
	m.LastLanguage = language
	if m.StreamFunc != nil {
		return m.StreamFunc(ctx, query, language, snippets)
	}
	if m.StartErr != nil {
		return nil, m.StartErr
	}
	out := make(chan SynthesisEvent, len(m.Script)+1)
	go func() {
		defer close(out)
		for _, ev := range m.Script {
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
		}
	}()
	return out, nil
}
