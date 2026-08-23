package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// MakeCoverageHandler wires the coverage handler.
//
// platformBaseURL is the deployment's publicly reachable root
// (`WEBHOOK_BASE_URL`), needed so the setup endpoint can tell a CI where to post.
// It is passed as a narrowed value rather than the whole *config.Config, which is
// the pattern the other factories follow (see scm.HostsOnly in routes.go).
func MakeCoverageHandler(repo storage.Repository, platformBaseURL string) *handlers.CoverageHandler {
	return handlers.NewCoverageHandler(services.NewCoverageService(repo), platformBaseURL)
}
