package redis

// Key builders for all Redis keys used in the application.
// Centralising them here prevents magic strings from spreading across packages.

const (
	prefixToken            = "token:"
	prefixUser             = "user:"
	prefixSession          = "session:"
	prefixRepo             = "repo:"
	prefixSearchSynthesis  = "synth:search:"
)

// TokenKey returns the cache key for a JWT token record keyed by JTI.
func TokenKey(jti string) string { return prefixToken + jti }

// UserKey returns the cache key for a user profile keyed by user ID.
func UserKey(id string) string { return prefixUser + id }

// SessionKey returns the cache key for a user session.
func SessionKey(id string) string { return prefixSession + id }

// RepoKey returns the cache key for a repository record keyed by ID.
func RepoKey(id string) string { return prefixRepo + id }

// SearchSynthesisKey returns the cache key for a stored AI synthesis tied to a
// (repository, query+snippet-set) fingerprint.
func SearchSynthesisKey(repoID, fingerprint string) string {
	return prefixSearchSynthesis + repoID + ":" + fingerprint
}
