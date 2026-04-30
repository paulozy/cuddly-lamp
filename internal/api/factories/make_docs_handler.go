package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeDocsHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *handlers.DocsHandler {
	return handlers.NewDocsHandler(repo, enqueuer)
}
