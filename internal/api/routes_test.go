package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/api/handlers"
)

// Gin builds a radix tree at registration time and panics when two routes put
// differently-named wildcards in the same position under the same parent. That
// happens at process start, so nothing short of actually registering the routes
// catches it — a unit test of a handler never will, and the failure mode is the
// server refusing to boot.
//
// The handlers are zero values: registration only needs the method values, and
// none of them is invoked here.
func TestSetupAPIRoutes_RegistersWithoutConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	noop := func(c *gin.Context) {}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("route registration panicked, the server would not start: %v", r)
		}
	}()

	setupAPIRoutes(
		router,
		&handlers.AuthHandler{},
		noop,
		&handlers.RepositoryHandler{},
		&handlers.RepositoryRelationshipHandler{},
		&handlers.WebhookHandler{},
		&handlers.PullRequestHandler{},
		&handlers.RepositoryBrowseHandler{},
		&handlers.DocsHandler{},
		&handlers.OrganizationConfigHandler{},
		&handlers.CoverageHandler{},
		&handlers.OrganizationMemberHandler{},
		&handlers.TeamHandler{},
		&handlers.OnboardingHandler{},
	)

	// The routes this change added must actually be present, not merely
	// conflict-free.
	want := map[string]string{
		http.MethodGet:  "/api/v1/repositories/:id/issues",
		http.MethodPost: "/api/v1/repositories/:id/pull-requests/:pr_number/approve",
	}
	registered := make(map[string]map[string]bool)
	for _, route := range router.Routes() {
		if registered[route.Method] == nil {
			registered[route.Method] = map[string]bool{}
		}
		registered[route.Method][route.Path] = true
	}
	for method, path := range want {
		if !registered[method][path] {
			t.Errorf("%s %s is not registered", method, path)
		}
	}
}
