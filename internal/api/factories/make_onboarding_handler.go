package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// MakeOnboardingHandler wires the configuration service and the runner. The
// runner gets a verifier, which is the only piece that talks to a provider —
// hosts carry the deployment's API roots and no tokens.
func MakeOnboardingHandler(repo storage.Repository, hosts scm.Credentials) *handlers.OnboardingHandler {
	verifier := services.NewOnboardingVerifier(repo, hosts)
	return handlers.NewOnboardingHandler(
		services.NewOnboardingService(repo),
		services.NewOnboardingRunService(repo, verifier),
	)
}
