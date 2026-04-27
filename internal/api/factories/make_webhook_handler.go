package factories

import (
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func MakeWebhookHandler(repo storage.Repository, enqueuer jobs.Enqueuer) *handlers.WebhookHandler {
	return handlers.NewWebhookHandler(repo, enqueuer)
}
