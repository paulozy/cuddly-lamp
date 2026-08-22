package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeOnboardingHandler(repo storage.Repository) *handlers.OnboardingHandler {
	return handlers.NewOnboardingHandler(services.NewOnboardingService(repo))
}
