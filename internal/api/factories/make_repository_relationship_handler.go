package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeRepositoryRelationshipHandler(repo storage.Repository) *handlers.RepositoryRelationshipHandler {
	relationshipService := services.NewRepositoryRelationshipService(repo)
	return handlers.NewRepositoryRelationshipHandler(relationshipService)
}
