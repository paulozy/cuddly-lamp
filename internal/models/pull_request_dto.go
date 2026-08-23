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
