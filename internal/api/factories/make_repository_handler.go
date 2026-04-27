package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
)

func MakeRepositoryHandler(repo storage.Repository, cache redis.Cache, enqueuer jobs.Enqueuer) *handlers.RepositoryHandler {
	repoService := services.NewRepositoryService(repo, cache, enqueuer)
	return handlers.NewRepositoryHandler(repoService)
}
