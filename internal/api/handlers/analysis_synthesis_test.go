package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	redisstore "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
)

// ─── stubs ────────────────────────────────────────────────────────────────────

type fakeCache struct {
	mu    sync.Mutex
	store map[string]string
}

func newFakeCache() *fakeCache { return &fakeCache{store: map[string]string{}} }

func (f *fakeCache) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.store[key]
	if !ok {
		return "", redisstore.ErrCacheMiss
	}
	return v, nil
}

func (f *fakeCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[key] = value
	return nil
}

func (f *fakeCache) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.store, k)
	}
	return nil
}

func (f *fakeCache) Exists(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.store[key]
	return ok, nil
}

type synthesisRepoStub struct {
	storage.Repository
	createdAnalyses []*models.CodeAnalysis
	createErr       error
}

func (s *synthesisRepoStub) CreateCodeAnalysis(_ context.Context, analysis *models.CodeAnalysis) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.createdAnalyses = append(s.createdAnalyses, analysis)
	return nil
}

func makeSSEContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/repositories/repo-1/search?q=foo&synthesize=true", nil)
	return c, w
}

func sampleResponse() models.SemanticSearchResponse {
	return models.SemanticSearchResponse{
		Query: "how does login work",
		Total: 1,
		Results: []models.SemanticSearchResult{{
			FilePath:  "internal/auth/login.go",
			Content:   "func Login() {}",
			Language:  "go",
			StartLine: 10,
			EndLine:   12,
			Score:     0.91,
		}},
	}
}

// ─── tests ────────────────────────────────────────────────────────────────────

func TestStreamSearchSynthesis_NoAnthropicKey_EmitsUnavailable(t *testing.T) {
	repo := &synthesisRepoStub{}
	h := NewAnalysisHandler(repo, nil, nil, nil)

	c, w := makeSSEContext()
	h.streamSearchSynthesis(c, &models.Repository{ID: "repo-1", OrganizationID: "org-1"}, "q", sampleResponse(), &models.OrganizationConfig{})

	body := w.Body.String()
	if !strings.Contains(body, "event:results") {
		t.Errorf("expected results event in body:\n%s", body)
	}
	if !strings.Contains(body, "event:synthesis_unavailable") {
		t.Errorf("expected synthesis_unavailable event in body:\n%s", body)
	}
	if !strings.Contains(body, "anthropic_not_configured") {
		t.Errorf("expected reason anthropic_not_configured in body:\n%s", body)
	}
	if !strings.Contains(body, "event:done") {
		t.Errorf("expected done event in body:\n%s", body)
	}
	if len(repo.createdAnalyses) != 0 {
		t.Errorf("expected no analyses persisted, got %d", len(repo.createdAnalyses))
	}
}

func TestStreamSearchSynthesis_CacheHit_EmitsSingleSynthesisEvent(t *testing.T) {
	repo := &synthesisRepoStub{}
	cache := newFakeCache()

	response := sampleResponse()
	fingerprint := computeSearchSynthesisFingerprint(response.Query, response.Results, searchSynthesisModelTag)
	cache.store[redisstore.SearchSynthesisKey("repo-1", fingerprint)] = "cached synthesis text"

	factoryCalls := 0
	factory := func(_ string) ai.Synthesizer {
		factoryCalls++
		return &ai.MockSynthesizer{}
	}
	h := NewAnalysisHandler(repo, nil, cache, factory)

	c, w := makeSSEContext()
	cfg := &models.OrganizationConfig{AnthropicAPIKey: "key", AnthropicTokensPerHour: 20000}
	h.streamSearchSynthesis(c, &models.Repository{ID: "repo-1", OrganizationID: "org-1"}, response.Query, response, cfg)

	body := w.Body.String()
	if !strings.Contains(body, "event:synthesis") {
		t.Errorf("expected synthesis event in body:\n%s", body)
	}
	if !strings.Contains(body, "cached synthesis text") {
		t.Errorf("expected cached synthesis text in body:\n%s", body)
	}
	if !strings.Contains(body, `"cached":true`) {
		t.Errorf("expected cached:true in done event:\n%s", body)
	}
	if factoryCalls != 0 {
		t.Errorf("expected synthesizer factory NOT to be called on cache hit, got %d calls", factoryCalls)
	}
	if len(repo.createdAnalyses) != 0 {
		t.Errorf("expected no analyses persisted on cache hit, got %d", len(repo.createdAnalyses))
	}
}

func TestStreamSearchSynthesis_CacheMiss_StreamsTokenDeltasAndPersists(t *testing.T) {
	repo := &synthesisRepoStub{}
	cache := newFakeCache()

	mock := &ai.MockSynthesizer{
		Script: []ai.SynthesisEvent{
			{Kind: ai.SynthesisEventTextDelta, Text: "Hello "},
			{Kind: ai.SynthesisEventTextDelta, Text: "world."},
			{Kind: ai.SynthesisEventUsage, Usage: &ai.SynthesisUsage{InputTokens: 100, OutputTokens: 25, Model: "claude-haiku-4-5-20251001"}},
			{Kind: ai.SynthesisEventDone},
		},
	}
	factory := func(apiKey string) ai.Synthesizer {
		if apiKey != "key" {
			t.Errorf("synthesizer factory got apiKey %q, want %q", apiKey, "key")
		}
		return mock
	}
	h := NewAnalysisHandler(repo, nil, cache, factory)

	c, w := makeSSEContext()
	cfg := &models.OrganizationConfig{AnthropicAPIKey: "key", AnthropicTokensPerHour: 20000}
	response := sampleResponse()
	h.streamSearchSynthesis(c, &models.Repository{ID: "repo-1", OrganizationID: "org-1"}, response.Query, response, cfg)

	body := w.Body.String()
	if !strings.Contains(body, "event:results") {
		t.Errorf("expected results event in body:\n%s", body)
	}
	tokenDeltas := strings.Count(body, "event:token_delta")
	if tokenDeltas != 2 {
		t.Errorf("expected 2 token_delta events, got %d\nbody:\n%s", tokenDeltas, body)
	}
	if !strings.Contains(body, `"text":"Hello "`) || !strings.Contains(body, `"text":"world."`) {
		t.Errorf("expected token deltas to include scripted text:\n%s", body)
	}
	if !strings.Contains(body, `"cached":false`) {
		t.Errorf("expected cached:false in done event:\n%s", body)
	}
	if !strings.Contains(body, `"tokens_used":125`) {
		t.Errorf("expected tokens_used:125 in done event:\n%s", body)
	}

	fingerprint := computeSearchSynthesisFingerprint(response.Query, response.Results, searchSynthesisModelTag)
	cached, err := cache.Get(context.Background(), redisstore.SearchSynthesisKey("repo-1", fingerprint))
	if err != nil {
		t.Fatalf("expected synthesis to be cached, got err: %v", err)
	}
	if cached != "Hello world." {
		t.Errorf("cached text = %q, want %q", cached, "Hello world.")
	}

	if len(repo.createdAnalyses) != 1 {
		t.Fatalf("expected 1 analysis persisted, got %d", len(repo.createdAnalyses))
	}
	got := repo.createdAnalyses[0]
	if got.Type != models.AnalysisTypeSearchSynthesis {
		t.Errorf("analysis Type = %q, want %q", got.Type, models.AnalysisTypeSearchSynthesis)
	}
	if got.TokensUsed != 125 {
		t.Errorf("analysis TokensUsed = %d, want 125", got.TokensUsed)
	}
	if got.Status != models.AnalysisStatusCompleted {
		t.Errorf("analysis Status = %q, want %q", got.Status, models.AnalysisStatusCompleted)
	}
	if got.RepositoryID != "repo-1" {
		t.Errorf("analysis RepositoryID = %q, want repo-1", got.RepositoryID)
	}
}

func TestStreamSearchSynthesis_StreamError_EmitsErrorEvent(t *testing.T) {
	repo := &synthesisRepoStub{}
	cache := newFakeCache()
	mock := &ai.MockSynthesizer{
		Script: []ai.SynthesisEvent{
			{Kind: ai.SynthesisEventTextDelta, Text: "partial"},
			{Kind: ai.SynthesisEventError, Err: errors.New("upstream timeout")},
		},
	}
	factory := func(_ string) ai.Synthesizer { return mock }
	h := NewAnalysisHandler(repo, nil, cache, factory)

	c, w := makeSSEContext()
	cfg := &models.OrganizationConfig{AnthropicAPIKey: "key", AnthropicTokensPerHour: 20000}
	h.streamSearchSynthesis(c, &models.Repository{ID: "repo-1", OrganizationID: "org-1"}, "q", sampleResponse(), cfg)

	body := w.Body.String()
	if !strings.Contains(body, "event:error") {
		t.Errorf("expected error event in body:\n%s", body)
	}
	if !strings.Contains(body, "synthesis_stream_failed") {
		t.Errorf("expected synthesis_stream_failed reason:\n%s", body)
	}
	if len(repo.createdAnalyses) != 0 {
		t.Errorf("expected no analyses persisted on stream error, got %d", len(repo.createdAnalyses))
	}
}

func TestStreamSearchSynthesis_SynthesizerStartFails_EmitsErrorEvent(t *testing.T) {
	repo := &synthesisRepoStub{}
	cache := newFakeCache()
	mock := &ai.MockSynthesizer{StartErr: errors.New("network refused")}
	factory := func(_ string) ai.Synthesizer { return mock }
	h := NewAnalysisHandler(repo, nil, cache, factory)

	c, w := makeSSEContext()
	cfg := &models.OrganizationConfig{AnthropicAPIKey: "key", AnthropicTokensPerHour: 20000}
	h.streamSearchSynthesis(c, &models.Repository{ID: "repo-1", OrganizationID: "org-1"}, "q", sampleResponse(), cfg)

	body := w.Body.String()
	if !strings.Contains(body, "event:error") {
		t.Errorf("expected error event in body:\n%s", body)
	}
	if !strings.Contains(body, "synthesis_start_failed") {
		t.Errorf("expected synthesis_start_failed reason:\n%s", body)
	}
}

func TestComputeSearchSynthesisFingerprint_StableForSameInput(t *testing.T) {
	results := []models.SemanticSearchResult{
		{FilePath: "a.go", StartLine: 1, EndLine: 10},
		{FilePath: "b.go", StartLine: 5, EndLine: 20},
	}
	a := computeSearchSynthesisFingerprint("login", results, "model-x")
	b := computeSearchSynthesisFingerprint("login", results, "model-x")
	if a != b {
		t.Error("fingerprint must be stable for same input")
	}
}

func TestComputeSearchSynthesisFingerprint_OrderIndependent(t *testing.T) {
	r1 := []models.SemanticSearchResult{
		{FilePath: "a.go", StartLine: 1, EndLine: 10},
		{FilePath: "b.go", StartLine: 5, EndLine: 20},
	}
	r2 := []models.SemanticSearchResult{
		{FilePath: "b.go", StartLine: 5, EndLine: 20},
		{FilePath: "a.go", StartLine: 1, EndLine: 10},
	}
	if computeSearchSynthesisFingerprint("q", r1, "m") != computeSearchSynthesisFingerprint("q", r2, "m") {
		t.Error("fingerprint should not depend on snippet order")
	}
}

func TestComputeSearchSynthesisFingerprint_DiffersOnQueryChange(t *testing.T) {
	results := []models.SemanticSearchResult{{FilePath: "a.go", StartLine: 1, EndLine: 10}}
	a := computeSearchSynthesisFingerprint("login", results, "m")
	b := computeSearchSynthesisFingerprint("logout", results, "m")
	if a == b {
		t.Error("fingerprint must differ when query changes")
	}
}

func TestComputeSearchSynthesisFingerprint_DiffersOnSnippetSetChange(t *testing.T) {
	results1 := []models.SemanticSearchResult{{FilePath: "a.go", StartLine: 1, EndLine: 10}}
	results2 := []models.SemanticSearchResult{{FilePath: "a.go", StartLine: 1, EndLine: 11}}
	if computeSearchSynthesisFingerprint("q", results1, "m") == computeSearchSynthesisFingerprint("q", results2, "m") {
		t.Error("fingerprint must differ when snippet line range changes")
	}
}

func TestSnippetsFromResults_PreservesAllFields(t *testing.T) {
	in := []models.SemanticSearchResult{{
		FilePath:  "a.go",
		Content:   "x",
		Language:  "go",
		StartLine: 1,
		EndLine:   2,
		Score:     0.7,
	}}
	got := snippetsFromResults(in)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	want := ai.SearchSnippet{FilePath: "a.go", Content: "x", Language: "go", StartLine: 1, EndLine: 2, Score: 0.7}
	if got[0] != want {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

func TestTruncateString(t *testing.T) {
	cases := []struct {
		in  string
		max int
		out string
	}{
		{"abc", 5, "abc"},
		{"abcdef", 3, "abc"},
		{"abc", 0, "abc"},
	}
	for _, tc := range cases {
		got := truncateString(tc.in, tc.max)
		if got != tc.out {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.out)
		}
	}
}
