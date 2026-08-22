package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeDocsHandler(repo storage.Repository, enqueuer jobs.Enqueuer, hosts scm.Credentials) *handlers.DocsHandler {
	return handlers.NewDocsHandler(repo, enqueuer, hosts)
}
