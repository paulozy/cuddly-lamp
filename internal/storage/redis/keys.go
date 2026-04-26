package redis

// Key builders for all Redis keys used in the application.
// Centralising them here prevents magic strings from spreading across packages.

const (
	prefixToken   = "token:"
	prefixUser    = "user:"
	prefixSession = "session:"
)

// TokenKey returns the cache key for a JWT token record keyed by JTI.
func TokenKey(jti string) string { return prefixToken + jti }

// UserKey returns the cache key for a user profile keyed by user ID.
func UserKey(id string) string { return prefixUser + id }

// SessionKey returns the cache key for a user session.
func SessionKey(id string) string { return prefixSession + id }
