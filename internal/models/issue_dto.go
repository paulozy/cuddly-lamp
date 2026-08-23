package models

// IssueResponse is one open issue on the repository's host.
//
// Counts carry no `omitempty`: a zero comment count is a fact, and dropping
// the key would make a required field vanish from the payload — the exact
// shape that has already produced silent empty states in this codebase.
type IssueResponse struct {
	Number        int64    `json:"number"`
	Title         string   `json:"title"`
	State         string   `json:"state"`
	AuthorLogin   string   `json:"author_login"`
	Labels        []string `json:"labels"`
	CommentsCount int      `json:"comments_count"`
	HTMLURL       string   `json:"html_url"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type IssueListResponse struct {
	Items []IssueResponse `json:"items"`
	Total int             `json:"total"`
}
