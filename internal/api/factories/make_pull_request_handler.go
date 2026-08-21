package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakePullRequestHandler(repo storage.Repository, hosts scm.Credentials) *handlers.PullRequestHandler {
	return handlers.NewPullRequestHandler(repo, hosts)
}
