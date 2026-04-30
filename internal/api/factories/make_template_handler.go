package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeTemplateHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *handlers.TemplateHandler {
	return handlers.NewTemplateHandler(repo, enqueuer)
}
