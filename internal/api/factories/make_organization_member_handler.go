package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeOrganizationMemberHandler(repo storage.Repository) *handlers.OrganizationMemberHandler {
	return handlers.NewOrganizationMemberHandler(services.NewMembershipService(repo))
}
