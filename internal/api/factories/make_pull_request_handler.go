package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakePullRequestHandler(repo storage.Repository) *handlers.PullRequestHandler {
	return handlers.NewPullRequestHandler(repo)
}
