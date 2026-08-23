package models

type PullRequestResponse struct {
	ID          int64  `json:"id"`
	Number      int64  `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body,omitempty"`
	State       string `json:"state"`
	AuthorLogin string `json:"author_login"`
	HeadBranch  string `json:"head_branch"`
	HeadSHA     string `json:"head_sha,omitempty"`
	BaseBranch  string `json:"base_branch"`
	BaseSHA     string `json:"base_sha,omitempty"`
	Draft       bool   `json:"draft"`
	// Null when the provider did not report the number. Distinct from 0, which
	// is a measured "nothing changed"; see scm.ChangeRequest.
	CommitsCount   *int   `json:"commits_count"`
	ChangedFiles   *int   `json:"changed_files"`
	AdditionsCount *int   `json:"additions_count"`
	DeletionsCount *int   `json:"deletions_count"`
	HTMLURL        string `json:"html_url"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	MergedAt       string `json:"merged_at,omitempty"`
	// ReviewBlockedReason names why the caller cannot review this change
	// request, or is null when nothing is known to stop them.
	//
	// Deliberately without `omitempty`: a numeric or boolean field that
	// disappears from the JSON when it holds its zero value has bitten this
	// codebase before (see the sibling *int fields, and FOLLOWUPS.md). Null and
	// absent must not be the same thing here — absent would read as "reviewable"
	// on a client that treats the key as required.
	//
	// This is advisory. It catches the case the platform can see for itself; it
	// is not a permission check, and the host remains the authority. See
	// ReviewBlocked* in the handlers.
	ReviewBlockedReason *string `json:"review_blocked_reason"`
	// ReviewDecision is the current verdict: "approved",
	// "changes_requested", "commented", or "" when nobody has reviewed.
	//
	// Null means the host could not be asked — distinct from "" which is a
	// measured "nobody reviewed yet". Do not collapse the two: null must render
	// as nothing, never as "not reviewed".
	ReviewDecision *string `json:"review_decision"`
	// Who holds each position. Empty rather than null when the decision is
	// known, so a client can iterate without a nil check.
	ApprovedBy         []string `json:"approved_by,omitempty"`
	ChangesRequestedBy []string `json:"changes_requested_by,omitempty"`
}

type PullRequestListItemResponse struct {
	PullRequest PullRequestResponse `json:"pull_request"`
}

type PullRequestListResponse struct {
	Items []PullRequestListItemResponse `json:"items"`
	Total int                           `json:"total"`
}

type PullRequestDetailResponse struct {
	PullRequest PullRequestResponse       `json:"pull_request"`
	Files       []PullRequestFileResponse `json:"files"`
}

type PullRequestFilesResponse struct {
	Items []PullRequestFileResponse `json:"items"`
	Total int                       `json:"total"`
}

type PullRequestFileResponse struct {
	SHA       string `json:"sha"`
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch,omitempty"`
}
