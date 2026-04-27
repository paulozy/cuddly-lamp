package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ValidateWebhookSignature checks the X-Hub-Signature-256 header against the
// HMAC-SHA256 of the request body using the webhook secret.
func ValidateWebhookSignature(secret string, body []byte, signature string) bool {
	if signature == "" || secret == "" {
		return false
	}

	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}

	hexSig := strings.TrimPrefix(signature, prefix)
	sigBytes, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(expected, sigBytes)
}
