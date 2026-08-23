package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/services"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// scmResolver turns a repository id from the URL into a provider client that
// speaks to that repository's own host.
//
// Every endpoint that proxies a source-code host needs the same four steps —
// load the repository, check it belongs to the caller's organization, parse
// its URL into a provider and a ref, and build a client from the
// organization's token — and each step has a specific failure response.
// Handlers embed this rather than repeating them.
type scmResolver struct {
	repo storage.Repository
	// hosts carries the deployment's provider API roots, with no tokens — see
	// scm.HostsOnly for why the two are treated differently.
	hosts   scm.Credentials
	resolve scm.ResolverFunc
	// authz evaluates repository ownership. Only CanWriteRepository is used, and
	// it reads nothing but the store, so the cache and enqueuer are nil.
	authz *services.RepositoryService
}

func newSCMResolver(repo storage.Repository, hosts scm.Credentials) *scmResolver {
	return &scmResolver{
		repo:    repo,
		hosts:   hosts,
		resolve: scm.For,
		authz:   services.NewRepositoryService(repo, nil, nil),
	}
}

// requireWriteAccess is the third authorization layer for actions that mutate
// the repository on its host.
//
// The role gate in middleware and the organization-scope check in
// resolveRepository have both already run; this adds the ownership rule, so a
// developer can act on the repositories their teams own without the whole
// organization needing maintainer.
func (r *scmResolver) requireWriteAccess(c *gin.Context, repository *models.Repository) bool {
	actor := actorFromContext(c)
	if r.authz.CanWriteRepository(c.Request.Context(), actor.Role, actor.UserID, repository) {
		return true
	}
	c.JSON(http.StatusForbidden, models.ErrorResponse{
		Error:            "forbidden",
		ErrorDescription: "you do not have permission to act on this repository",
	})
	return false
}

// resolvedRepo is what a successful resolution yields: the stored repository,
// the provider client, and the ref to address it by.
type resolvedRepo struct {
	repository *models.Repository
	provider   scm.Provider
	ref        scm.RepoRef
	// kind is the provider the repository's URL resolved to, which is not
	// necessarily repository.Type — the URL wins, see resolveRepository. Kept
	// here so callers that need to name the host (looking up the caller's OAuth
	// identity on it, for instance) do not re-parse the URL.
	kind models.RepositoryType
}

// resolveRepository runs the full chain, writing the appropriate error
// response and returning false if any step fails.
func (r *scmResolver) resolveRepository(c *gin.Context, repoID string) (*resolvedRepo, bool) {
	repository, ok := r.fetchAccessibleRepository(c, repoID)
	if !ok {
		return nil, false
	}

	// The host in the URL decides which provider is queried, not the stored
	// type — the URL is what the user gave us and it is never empty.
	projectPath, provider, err := utils.ParseRepositoryURL(repository.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_repository_url",
			ErrorDescription: err.Error(),
		})
		return nil, false
	}
	ref, err := scm.ParseRepoRef(projectPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_repository_url",
			ErrorDescription: "repository URL must identify a namespace and a repository",
		})
		return nil, false
	}

	cfg, err := r.repo.GetOrganizationConfig(c.Request.Context(), repository.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch organization config",
		})
		return nil, false
	}

	// The organization's own token, never the platform's — browsing another
	// organization's code with the deployment's credentials is not something a
	// read endpoint should silently do. The host does fall back, because
	// talking to the wrong GitLab is worse than talking to none.
	client, err := r.resolve(provider, scm.CredentialsFromConfig(cfg, r.hosts))
	if err != nil {
		r.providerError(c, err)
		return nil, false
	}

	return &resolvedRepo{repository: repository, provider: client, ref: ref, kind: provider}, true
}

// fetchAccessibleRepository loads a repository and refuses it unless it
// belongs to the caller's organization.
func (r *scmResolver) fetchAccessibleRepository(c *gin.Context, repoID string) (*models.Repository, bool) {
	if repoID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "repository id is required",
		})
		return nil, false
	}

	repository, err := r.repo.GetRepository(c.Request.Context(), repoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:            "internal_error",
			ErrorDescription: "failed to fetch repository",
		})
		return nil, false
	}
	if repository == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "repository not found",
		})
		return nil, false
	}

	orgID, err := utils.GetOrganizationIDFromContext(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:            "unauthorized",
			ErrorDescription: "missing or invalid authentication",
		})
		return nil, false
	}
	if repository.OrganizationID != orgID {
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Error:            "forbidden",
			ErrorDescription: "you do not have access to this repository",
		})
		return nil, false
	}

	return repository, true
}

// providerError maps the canonical scm errors onto status codes.
//
// Provider failures are reported as 503 rather than 502: from the caller's
// side an expired token and a rate limit are both "this integration cannot
// answer right now", and both are resolved by acting on the organization's
// configuration, not by retrying blindly.
func (r *scmResolver) providerError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, scm.ErrUnsupportedProvider):
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "unsupported_repository_type",
			ErrorDescription: err.Error(),
		})
	case errors.Is(err, scm.ErrMissingCredentials):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_unavailable",
			ErrorDescription: "no access token is configured for this repository's provider",
		})
	case errors.Is(err, scm.ErrNotFound):
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:            "not_found",
			ErrorDescription: "provider resource not found",
		})
	case errors.Is(err, scm.ErrUnauthorized):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_unavailable",
			ErrorDescription: "the configured provider token is invalid or unauthorized",
		})
	case errors.Is(err, scm.ErrRateLimited):
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_rate_limited",
			ErrorDescription: "provider API rate limit exceeded",
		})
	case errors.Is(err, scm.ErrSelfReview):
		// 409, not 503: the host understood the request and refused it. The
		// action can never succeed as sent, so reporting it as a temporary
		// outage would invite a retry that cannot work.
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "self_review",
			ErrorDescription: selfReviewDescription(err),
		})
	case errors.Is(err, scm.ErrProviderRejected):
		// Every other refusal the host spelled out: already approved, no
		// commits between branches, a duplicate. Its own words are the most
		// useful thing we can pass on, and they are already truncated.
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:            "provider_rejected",
			ErrorDescription: providerRejectionDescription(err),
		})
	default:
		// Deliberately not err.Error(): an unclassified failure is the one case
		// where the underlying text is least likely to mean anything to a
		// caller, and most likely to leak a raw provider payload.
		c.JSON(http.StatusServiceUnavailable, models.ErrorResponse{
			Error:            "provider_unavailable",
			ErrorDescription: "the repository's host could not complete this request",
		})
	}
}

// selfReviewDescription explains the refusal in the platform's own terms.
//
// The host's wording ("Can not approve your own pull request") is technically
// true and practically confusing here, because the identity that authored the
// change request is usually the organization's configured token rather than the
// person clicking. Saying so is the difference between a dead end and a fix.
func selfReviewDescription(err error) string {
	var providerErr *scm.ProviderError
	owner := ""
	if errors.As(err, &providerErr) {
		owner = providerErr.TokenOwner
	}
	if owner != "" {
		return "this change request was opened by @" + owner + ", the identity this organization's token belongs to — a review has to come from someone else"
	}
	return "the identity this organization's token belongs to opened this change request, and a host will not let an author review their own work"
}

// providerRejectionDescription passes the host's own explanation through,
// falling back to a neutral sentence when there is none.
func providerRejectionDescription(err error) string {
	var providerErr *scm.ProviderError
	if errors.As(err, &providerErr) && providerErr.Message != "" {
		return providerErr.Message
	}
	return "the repository's host refused this action"
}
