package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// contributorCommitWindow is how many recent commits are scanned to date a
// contributor's last activity.
//
// The contributors endpoint reports no timestamp on either provider, so the
// only honest source is the commit log — and it has to be bounded. A
// contributor whose last commit falls outside this window reports nil rather
// than a wrong date.
const contributorCommitWindow = 100

// RepositoryBrowseHandler serves the read-only repository views that proxy the
// host but are not change requests: issues and contributors.
//
// Like PullRequestHandler it holds no queue and no cache — each response is a
// pass-through of the host's API using the repository organization's token.
type RepositoryBrowseHandler struct {
	*scmResolver
}

func NewRepositoryBrowseHandler(repo storage.Repository, hosts scm.Credentials) *RepositoryBrowseHandler {
	return &RepositoryBrowseHandler{scmResolver: newSCMResolver(repo, hosts)}
}

// ListIssues lists open issues for a repository.
// @Summary      List repository issues
// @Tags         issues
// @Produce      json
// @Security     BearerAuth
// @Param        id  path      string  true  "Repository ID"
// @Success      200 {object}  models.IssueListResponse
// @Failure      400 {object}  models.ErrorResponse
// @Failure      401 {object}  models.ErrorResponse
// @Failure      403 {object}  models.ErrorResponse
// @Failure      404 {object}  models.ErrorResponse
// @Failure      503 {object}  models.ErrorResponse
// @Router       /repositories/{id}/issues [get]
func (h *RepositoryBrowseHandler) ListIssues(c *gin.Context) {
	resolved, ok := h.resolveRepository(c, c.Param("id"))
	if !ok {
		return
	}

	issues, err := resolved.provider.ListIssues(c.Request.Context(), resolved.ref)
	if err != nil {
		h.providerError(c, err)
		return
	}

	items := make([]models.IssueResponse, 0, len(issues))
	for _, issue := range issues {
		labels := issue.Labels
		if labels == nil {
			// A nil slice marshals to null; the client's schema expects a list.
			labels = []string{}
		}
		items = append(items, models.IssueResponse{
			Number:        issue.Number,
			Title:         issue.Title,
			State:         issue.State,
			AuthorLogin:   issue.AuthorLogin,
			Labels:        labels,
			CommentsCount: issue.CommentsCount,
			HTMLURL:       issue.WebURL,
			CreatedAt:     issue.CreatedAt,
			UpdatedAt:     issue.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, models.IssueListResponse{Items: items, Total: len(items)})
}

// ListContributors lists people credited with commits on a repository.
// @Summary      List repository contributors
// @Tags         contributors
// @Produce      json
// @Security     BearerAuth
// @Param        id  path      string  true  "Repository ID"
// @Success      200 {object}  models.ContributorListResponse
// @Failure      400 {object}  models.ErrorResponse
// @Failure      401 {object}  models.ErrorResponse
// @Failure      403 {object}  models.ErrorResponse
// @Failure      404 {object}  models.ErrorResponse
// @Failure      503 {object}  models.ErrorResponse
// @Router       /repositories/{id}/contributors [get]
func (h *RepositoryBrowseHandler) ListContributors(c *gin.Context) {
	resolved, ok := h.resolveRepository(c, c.Param("id"))
	if !ok {
		return
	}

	contributors, err := resolved.provider.ListContributors(c.Request.Context(), resolved.ref)
	if err != nil {
		h.providerError(c, err)
		return
	}

	openByAuthor := h.openChangeRequestsByAuthor(c, resolved)
	lastCommitByAuthor := h.lastCommitByAuthor(c, resolved)

	items := make([]models.ContributorResponse, 0, len(contributors))
	for _, contributor := range contributors {
		items = append(items, models.ContributorResponse{
			Login:     contributor.Login,
			Name:      contributor.Name,
			AvatarURL: contributor.AvatarURL,
			Commits:   contributor.Commits,
			// Both are looked up by whichever identity the contributor carries.
			// A miss stays nil — see ContributorResponse for why that is not 0.
			OpenChangeRequests: lookupInt(openByAuthor, contributor),
			LastCommitAt:       lookupString(lastCommitByAuthor, contributor),
		})
	}

	c.JSON(http.StatusOK, models.ContributorListResponse{Items: items, Total: len(items)})
}

// CloseIssue closes an open issue on the repository's host.
// @Summary      Close a repository issue
// @Tags         issues
// @Produce      json
// @Security     BearerAuth
// @Param        id      path      string  true  "Repository ID"
// @Param        number  path      int     true  "Issue number"
// @Success      204     "closed"
// @Failure      400     {object}  models.ErrorResponse
// @Failure      401     {object}  models.ErrorResponse
// @Failure      403     {object}  models.ErrorResponse
// @Failure      404     {object}  models.ErrorResponse
// @Failure      503     {object}  models.ErrorResponse
// @Router       /repositories/{id}/issues/{number}/close [post]
func (h *RepositoryBrowseHandler) CloseIssue(c *gin.Context) {
	number, ok := parsePositivePathInt(c, "number", "issue number")
	if !ok {
		return
	}
	resolved, ok := h.resolveRepository(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.requireWriteAccess(c, resolved.repository) {
		return
	}

	if err := resolved.provider.CloseIssue(c.Request.Context(), resolved.ref, number); err != nil {
		h.providerError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// parsePositivePathInt reads a positive integer path parameter.
func parsePositivePathInt(c *gin.Context, param, label string) (int64, bool) {
	value, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || value <= 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: label + " must be a positive integer",
		})
		return 0, false
	}
	return value, true
}

// openChangeRequestsByAuthor counts open change requests per author login.
//
// A failure here is deliberately not fatal: the contributor list is still
// useful without the derived counts, and degrading to "unknown" beats failing
// the whole request over an enrichment.
func (h *RepositoryBrowseHandler) openChangeRequestsByAuthor(c *gin.Context, resolved *resolvedRepo) map[string]int {
	crs, err := resolved.provider.ListChangeRequests(c.Request.Context(), resolved.ref)
	if err != nil {
		utils.Warn("browse: could not derive change-request counts",
			"repository_id", resolved.repository.ID, "error", err)
		return nil
	}
	counts := make(map[string]int, len(crs))
	for _, cr := range crs {
		if key := identityKey(cr.AuthorLogin); key != "" {
			counts[key]++
		}
	}
	return counts
}

// lastCommitByAuthor dates each author's most recent commit within
// contributorCommitWindow. Commits arrive newest first, so the first sighting
// of an author wins.
func (h *RepositoryBrowseHandler) lastCommitByAuthor(c *gin.Context, resolved *resolvedRepo) map[string]string {
	branch := resolved.repository.Metadata.DefaultBranch
	commits, err := resolved.provider.ListCommits(c.Request.Context(), resolved.ref, branch, contributorCommitWindow)
	if err != nil {
		utils.Warn("browse: could not derive last commit dates",
			"repository_id", resolved.repository.ID, "error", err)
		return nil
	}
	last := make(map[string]string, len(commits))
	for _, commit := range commits {
		key := identityKey(commit.AuthorName)
		if key == "" {
			continue
		}
		if _, seen := last[key]; seen {
			continue
		}
		last[key] = commit.Date.UTC().Format("2006-01-02T15:04:05Z")
	}
	return last
}

// identityKey normalizes a name or login for matching.
func identityKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// contributorKeys are the identities a contributor might be known by on the
// other endpoints. GitHub keys change requests on the login; GitLab keys
// commits on the display name. Trying both is what lets one lookup serve both
// providers without pretending either identity is universal.
func contributorKeys(contributor scm.Contributor) []string {
	keys := make([]string, 0, 2)
	if key := identityKey(contributor.Login); key != "" {
		keys = append(keys, key)
	}
	if key := identityKey(contributor.Name); key != "" {
		keys = append(keys, key)
	}
	return keys
}

func lookupInt(table map[string]int, contributor scm.Contributor) *int {
	if table == nil {
		return nil
	}
	for _, key := range contributorKeys(contributor) {
		if value, ok := table[key]; ok {
			return &value
		}
	}
	// The contributor is known and the table was built, so "no change requests
	// found" really does mean none are open.
	zero := 0
	return &zero
}

func lookupString(table map[string]string, contributor scm.Contributor) *string {
	if table == nil {
		return nil
	}
	for _, key := range contributorKeys(contributor) {
		if value, ok := table[key]; ok {
			return &value
		}
	}
	// Unlike the change-request count, absence here is genuinely ambiguous: the
	// author may simply have no commit inside the scanned window.
	return nil
}
