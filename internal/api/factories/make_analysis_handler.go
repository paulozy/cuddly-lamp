package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/anthropic"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	redisstore "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
)

func MakeAnalysisHandler(repo storage.Repository, enqueuer jobs.Enqueuer, cache redisstore.Cache) *handlers.AnalysisHandler {
	synthesizerFactory := func(apiKey string) ai.Synthesizer {
		if apiKey == "" {
			return nil
		}
		return anthropic.NewClient(apiKey)
	}
	return handlers.NewAnalysisHandler(repo, enqueuer, cache, synthesizerFactory)
}
