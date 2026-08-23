package models

// ContributorResponse is one person credited with commits on a repository.
//
// Login and Name are both optional because the two providers report disjoint
// halves of the identity: GitHub's contributors endpoint returns a username
// and no display name, GitLab's returns a display name and an email and no
// username. Clients render whichever arrived.
//
// OpenChangeRequests and LastCommitAt are pointers on purpose. Neither
// provider reports them on the contributors endpoint, so the handler derives
// them by matching the contributor against data we do fetch — and a match is
// not always possible, because the identity the contributors endpoint gives us
// is not always the identity the other endpoints key on.
//
// nil therefore means "could not be determined", which is a different claim
// from 0 ("determined, and it is none"). Collapsing the two would make the UI
// state confidently that someone has opened no change requests when the truth
// is that we had no way to tell — the same mistake `RepositoryMetadata.HasCI`
// exists as a pointer to avoid.
type ContributorResponse struct {
	Login              string  `json:"login,omitempty"`
	Name               string  `json:"name,omitempty"`
	AvatarURL          string  `json:"avatar_url,omitempty"`
	Commits            int     `json:"commits"`
	OpenChangeRequests *int    `json:"open_change_requests"`
	LastCommitAt       *string `json:"last_commit_at"`
}

type ContributorListResponse struct {
	Items []ContributorResponse `json:"items"`
	Total int                   `json:"total"`
}
