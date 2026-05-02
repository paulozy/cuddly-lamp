package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeCoverageHandler(repo storage.Repository) *handlers.CoverageHandler {
	return handlers.NewCoverageHandler(services.NewCoverageService(repo))
}
