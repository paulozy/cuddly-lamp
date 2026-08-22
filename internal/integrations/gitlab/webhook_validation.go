package gitlab

import "crypto/subtle"

// ValidateWebhookToken checks the X-Gitlab-Token header against the secret
// stored for the repository's webhook.
//
// GitLab sends the secret back as plain text rather than signing the body,
// so there is no HMAC to recompute — the check is a comparison, and it is done
// in constant time so a wrong token cannot be discovered byte by byte.
func ValidateWebhookToken(secret, received string) bool {
	if secret == "" || received == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(secret), []byte(received)) == 1
}
