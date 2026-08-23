package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// authStore answers the one lookup GetCurrentUser performs.
type authStore struct {
	storage.Repository
	org    *models.Organization
	orgErr error
}

func (s *authStore) GetOrganizationBySlug(_ context.Context, _ string) (*models.Organization, error) {
	return s.org, s.orgErr
}

func currentUserRequest(store storage.Repository, slug string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	handler := NewAuthHandler(services.NewAuthService(store, "secret", "iss", "aud", time.Minute, time.Hour))

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := context.WithValue(req.Context(), utils.ContextKeyUser, "user-1")
	ctx = context.WithValue(ctx, utils.ContextKeyClaims, &models.TokenClaims{
		UserID:           "user-1",
		Email:            "paulo@example.com",
		FullName:         "Paulo Abreu",
		OrganizationID:   "org-1",
		OrganizationSlug: slug,
		OrganizationRole: models.RoleAdmin,
	})
	c.Request = req.WithContext(ctx)

	handler.GetCurrentUser(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func decodeUser(t *testing.T, rec *httptest.ResponseRecorder) models.UserInfo {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	var got models.UserInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// The name is not on the token, so it has to be read. Without this the
// frontend fell back to a hardcoded "Organização" for every user, however
// their organization was actually named.
func TestGetCurrentUser_CarriesTheOrganizationName(t *testing.T) {
	store := &authStore{org: &models.Organization{Name: "Housi", Slug: "housi", IsActive: true}}

	got := decodeUser(t, currentUserRequest(store, "housi"))
	if got.Organization == nil {
		t.Fatal("organization is nil")
	}
	if got.Organization.Name != "Housi" {
		t.Errorf("name = %q, want Housi", got.Organization.Name)
	}
	if got.Organization.ID != "org-1" || got.Organization.Role != models.RoleAdmin {
		t.Errorf("id/role = %q/%q, want org-1/admin", got.Organization.ID, got.Organization.Role)
	}
}

// A display name is not worth failing the whole session read over — the client
// falls back to a generic label, which is exactly the old behaviour.
func TestGetCurrentUser_SurvivesAFailedNameLookup(t *testing.T) {
	tests := []struct {
		name  string
		store *authStore
	}{
		{name: "lookup errored", store: &authStore{orgErr: errors.New("database is down")}},
		{name: "organization not found", store: &authStore{}},
		{name: "organization inactive", store: &authStore{org: &models.Organization{Name: "Housi", IsActive: false}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeUser(t, currentUserRequest(tt.store, "housi"))
			if got.Organization == nil {
				t.Fatal("organization is nil — the rest of the payload must survive")
			}
			if got.Organization.Name != "" {
				t.Errorf("name = %q, want empty so the client falls back", got.Organization.Name)
			}
			if got.Organization.ID != "org-1" {
				t.Errorf("id = %q, want org-1", got.Organization.ID)
			}
		})
	}
}

// No slug means nothing to look up, and no reason to hit the database.
func TestGetCurrentUser_SkipsTheLookupWithoutASlug(t *testing.T) {
	store := &authStore{org: &models.Organization{Name: "Housi", IsActive: true}}

	got := decodeUser(t, currentUserRequest(store, ""))
	if got.Organization.Name != "" {
		t.Errorf("name = %q, want empty when the token carries no slug", got.Organization.Name)
	}
}
