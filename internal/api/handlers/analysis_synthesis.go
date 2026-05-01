package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	redisstore "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

const (
	searchSynthesisCacheTTL = time.Hour
	searchSynthesisDeadline = 45 * time.Second
	searchSynthesisModelTag = "claude-haiku-4-5-20251001"
)

// streamSearchSynthesis upgrades the response to SSE and emits search results
// followed by an AI-generated synthesis (or a graceful fallback when AI is not
// configured). Token usage is persisted so it counts toward the org's budget.
func (h *AnalysisHandler) streamSearchSynthesis(
	c *gin.Context,
	repository *models.Repository,
	query string,
	response models.SemanticSearchResponse,
	orgConfig *models.OrganizationConfig,
) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	if rc := http.NewResponseController(c.Writer); rc != nil {
		_ = rc.SetWriteDeadline(time.Now().Add(searchSynthesisDeadline))
	}

	c.SSEvent("results", response)
	c.Writer.Flush()

	if orgConfig == nil || orgConfig.AnthropicAPIKey == "" {
		c.SSEvent("synthesis_unavailable", gin.H{"reason": "anthropic_not_configured"})
		c.SSEvent("done", gin.H{"cached": false, "tokens_used": 0})
		c.Writer.Flush()
		return
	}

	if h.synthesizerFactory == nil {
		c.SSEvent("synthesis_unavailable", gin.H{"reason": "synthesizer_not_wired"})
		c.SSEvent("done", gin.H{"cached": false, "tokens_used": 0})
		c.Writer.Flush()
		return
	}

	cache := h.cache
	if cache == nil {
		cache = redisstore.NewNoopCache()
	}

	fingerprint := computeSearchSynthesisFingerprint(query, response.Results, searchSynthesisModelTag, orgConfig.ResolvedOutputLanguage())
	cacheKey := redisstore.SearchSynthesisKey(repository.ID, fingerprint)

	if cached, err := cache.Get(c.Request.Context(), cacheKey); err == nil && cached != "" {
		c.SSEvent("synthesis", gin.H{"text": cached})
		c.SSEvent("done", gin.H{"cached": true, "tokens_used": 0})
		c.Writer.Flush()
		return
	} else if err != nil && !errors.Is(err, redisstore.ErrCacheMiss) {
		utils.Error("semantic search: cache get failed", "key", cacheKey, "error", err)
	}

	synthesizer := h.synthesizerFactory(orgConfig.AnthropicAPIKey)
	if synthesizer == nil {
		c.SSEvent("synthesis_unavailable", gin.H{"reason": "synthesizer_not_wired"})
		c.SSEvent("done", gin.H{"cached": false, "tokens_used": 0})
		c.Writer.Flush()
		return
	}

	snippets := snippetsFromResults(response.Results)
	streamCtx, cancel := context.WithTimeout(c.Request.Context(), searchSynthesisDeadline)
	defer cancel()

	events, err := synthesizer.StreamSearchSynthesis(streamCtx, query, orgConfig.ResolvedOutputLanguage(), snippets)
	if err != nil {
		utils.Error("semantic search: synthesizer start failed", "repo_id", repository.ID, "error", err)
		c.SSEvent("error", gin.H{"reason": "synthesis_start_failed"})
		c.SSEvent("done", gin.H{"cached": false, "tokens_used": 0})
		c.Writer.Flush()
		return
	}

	var (
		fullText  strings.Builder
		usage     *ai.SynthesisUsage
		streamErr error
		reqDone   = c.Request.Context().Done()
	)

readLoop:
	for {
		select {
		case <-reqDone:
			cancel()
			break readLoop
		case ev, ok := <-events:
			if !ok {
				break readLoop
			}
			switch ev.Kind {
			case ai.SynthesisEventTextDelta:
				fullText.WriteString(ev.Text)
				c.SSEvent("token_delta", gin.H{"text": ev.Text})
				c.Writer.Flush()
			case ai.SynthesisEventUsage:
				usage = ev.Usage
			case ai.SynthesisEventError:
				streamErr = ev.Err
			case ai.SynthesisEventDone:
				// terminal — no-op, channel will close next.
			}
		}
	}

	if streamErr != nil {
		utils.Error("semantic search: synthesis stream error", "repo_id", repository.ID, "error", streamErr)
		c.SSEvent("error", gin.H{"reason": "synthesis_stream_failed"})
		c.SSEvent("done", gin.H{"cached": false, "tokens_used": 0})
		c.Writer.Flush()
		return
	}

	tokensUsed := 0
	model := searchSynthesisModelTag
	if usage != nil {
		tokensUsed = usage.InputTokens + usage.OutputTokens
		if usage.Model != "" {
			model = usage.Model
		}
	}

	text := fullText.String()
	if text != "" {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := cache.Set(bgCtx, cacheKey, text, searchSynthesisCacheTTL); err != nil {
			utils.Error("semantic search: cache set failed", "key", cacheKey, "error", err)
		}
		bgCancel()

		h.persistSynthesisAnalysis(repository, query, model, tokensUsed)
	}

	c.SSEvent("done", gin.H{
		"cached":      false,
		"tokens_used": tokensUsed,
		"model":       model,
	})
	c.Writer.Flush()
}

// persistSynthesisAnalysis records a CodeAnalysis row of type search_synthesis
// so that the tokens used count toward SumTokensUsedSince.
func (h *AnalysisHandler) persistSynthesisAnalysis(repository *models.Repository, query, model string, tokensUsed int) {
	if tokensUsed <= 0 {
		return
	}
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	analysis := &models.CodeAnalysis{
		RepositoryID: repository.ID,
		Type:         models.AnalysisTypeSearchSynthesis,
		Status:       models.AnalysisStatusCompleted,
		Title:        truncateString("Semantic search synthesis: "+query, 500),
		TriggeredBy:  "user",
		IsAIAnalysis: true,
		AIModel:      model,
		TokensUsed:   tokensUsed,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := h.repo.CreateCodeAnalysis(bgCtx, analysis); err != nil {
		utils.Error("semantic search: persist synthesis analysis failed", "repo_id", repository.ID, "error", err)
	}
}

// snippetsFromResults converts API search results into ai.SearchSnippet inputs
// for the synthesizer.
func snippetsFromResults(results []models.SemanticSearchResult) []ai.SearchSnippet {
	out := make([]ai.SearchSnippet, len(results))
	for i, r := range results {
		out[i] = ai.SearchSnippet{
			FilePath:  r.FilePath,
			Content:   r.Content,
			Language:  r.Language,
			StartLine: r.StartLine,
			EndLine:   r.EndLine,
			Score:     r.Score,
		}
	}
	return out
}

// computeSearchSynthesisFingerprint returns a stable hash of the query and the
// ordered identity of the snippet set, scoped to the synthesizer model and the
// requested output language. The same query against the same snippet set will
// produce the same key for the same language; switching languages yields a
// different key so callers never see a synthesis cached in the wrong language.
func computeSearchSynthesisFingerprint(query string, results []models.SemanticSearchResult, model, language string) string {
	items := make([]string, len(results))
	for i, r := range results {
		items[i] = fmt.Sprintf("%s:%d-%d", r.FilePath, r.StartLine, r.EndLine)
	}
	sort.Strings(items)

	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(query)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.Join(items, ",")))
	h.Write([]byte("|"))
	h.Write([]byte(model))
	h.Write([]byte("|"))
	h.Write([]byte(strings.ToLower(strings.TrimSpace(language))))
	return hex.EncodeToString(h.Sum(nil))
}

func truncateString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
