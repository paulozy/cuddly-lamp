package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// newRoleRequest builds a request carrying the claims the auth middleware would
// have attached. Passing an empty role simulates a request that never went
// through the auth middleware at all.
func newRoleRequest(role models.UserRole, withClaims bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repositories", nil)
	if !withClaims {
		return req
	}
	claims := &models.TokenClaims{OrganizationRole: role}
	ctx := context.WithValue(req.Context(), utils.ContextKeyClaims, claims)
	return req.WithContext(ctx)
}

func runRoleGuard(t *testing.T, minRole models.UserRole, req *http.Request) (int, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	reached := false
	router := gin.New()
	router.POST("/api/v1/repositories", RequireRole(minRole), func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, reached
}

func TestRequireRole_AllowsEqualOrHigherRole(t *testing.T) {
	tests := []struct {
		name    string
		role    models.UserRole
		minRole models.UserRole
	}{
		{name: "exact match", role: models.RoleDeveloper, minRole: models.RoleDeveloper},
		{name: "maintainer over developer", role: models.RoleMaintainer, minRole: models.RoleDeveloper},
		{name: "admin over maintainer", role: models.RoleAdmin, minRole: models.RoleMaintainer},
		{name: "admin over developer", role: models.RoleAdmin, minRole: models.RoleDeveloper},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, reached := runRoleGuard(t, tt.minRole, newRoleRequest(tt.role, true))
			if code != http.StatusOK {
				t.Errorf("status = %d, want %d", code, http.StatusOK)
			}
			if !reached {
				t.Error("handler was not reached")
			}
		})
	}
}

// The regression this guards: the middleware existed but was never mounted, so
// a viewer could create, update and delete repositories.
func TestRequireRole_RejectsLowerRole(t *testing.T) {
	tests := []struct {
		name    string
		role    models.UserRole
		minRole models.UserRole
	}{
		{name: "viewer cannot write", role: models.RoleViewer, minRole: models.RoleDeveloper},
		{name: "viewer cannot maintain", role: models.RoleViewer, minRole: models.RoleMaintainer},
		{name: "developer cannot maintain", role: models.RoleDeveloper, minRole: models.RoleMaintainer},
		{name: "maintainer cannot administer", role: models.RoleMaintainer, minRole: models.RoleAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, reached := runRoleGuard(t, tt.minRole, newRoleRequest(tt.role, true))
			if code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", code, http.StatusForbidden)
			}
			if reached {
				t.Error("handler ran despite insufficient role")
			}
		})
	}
}

// An unrecognised role must not be treated as privileged: HasPermission maps
// unknown values to level 0, which sits below every real role.
func TestRequireRole_RejectsUnknownRole(t *testing.T) {
	code, reached := runRoleGuard(t, models.RoleViewer, newRoleRequest(models.UserRole("root"), true))
	if code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", code, http.StatusForbidden)
	}
	if reached {
		t.Error("handler ran for an unknown role")
	}
}

func TestRequireRole_RejectsMissingClaims(t *testing.T) {
	code, reached := runRoleGuard(t, models.RoleViewer, newRoleRequest("", false))
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", code, http.StatusUnauthorized)
	}
	if reached {
		t.Error("handler ran without claims on the context")
	}
}
