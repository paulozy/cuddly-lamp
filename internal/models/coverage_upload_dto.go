package models

import "time"

// CoverageUploadResponse is returned by POST /repositories/:id/coverage.
type CoverageUploadResponse struct {
	ID           string   `json:"id"`
	CommitSHA    string   `json:"commit_sha"`
	Format       string   `json:"format"`
	LinesCovered int      `json:"lines_covered"`
	LinesTotal   int      `json:"lines_total"`
	Percentage   float64  `json:"percentage"`
	Status       string   `json:"status"`
	Warnings     []string `json:"warnings,omitempty"`
}

// CreateCoverageTokenRequest is the body of POST /repositories/:id/coverage/tokens.
type CreateCoverageTokenRequest struct {
	Name      string     `json:"name" binding:"required"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateCoverageTokenResponse exposes the plaintext token exactly once.
// Subsequent reads via ListCoverageTokens never include it.
type CreateCoverageTokenResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"` // shown ONCE — backend stores only sha256(token)
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// CoverageTokenResponse is the row shape for List/Get endpoints.
type CoverageTokenResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func CoverageTokenToResponse(t *CoverageUploadToken) CoverageTokenResponse {
	return CoverageTokenResponse{
		ID:         t.ID,
		Name:       t.Name,
		LastUsedAt: t.LastUsedAt,
		ExpiresAt:  t.ExpiresAt,
		RevokedAt:  t.RevokedAt,
		CreatedAt:  t.CreatedAt,
	}
}
