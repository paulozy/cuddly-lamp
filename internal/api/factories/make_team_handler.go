package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeTeamHandler(repo storage.Repository) *handlers.TeamHandler {
	return handlers.NewTeamHandler(services.NewTeamService(repo))
}
