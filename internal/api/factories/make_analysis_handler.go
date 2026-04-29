package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeAnalysisHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *handlers.AnalysisHandler {
	return handlers.NewAnalysisHandler(repo, enqueuer)
}
