package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeRepositoryBrowseHandler(repo storage.Repository, hosts scm.Credentials) *handlers.RepositoryBrowseHandler {
	return handlers.NewRepositoryBrowseHandler(repo, hosts)
}
