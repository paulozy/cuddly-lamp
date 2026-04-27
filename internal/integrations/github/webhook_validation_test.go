package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestValidateWebhookSignature(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"action":"opened"}`)

	tests := []struct {
		name      string
		secret    string
		body      []byte
		signature string
		want      bool
	}{
		{
			name:      "valid signature",
			secret:    secret,
			body:      body,
			signature: signBody(secret, body),
			want:      true,
		},
		{
			name:      "wrong secret",
			secret:    "other-secret",
			body:      body,
			signature: signBody(secret, body),
			want:      false,
		},
		{
			name:      "tampered body",
			secret:    secret,
			body:      []byte(`{"action":"closed"}`),
			signature: signBody(secret, body),
			want:      false,
		},
		{
			name:      "empty signature",
			secret:    secret,
			body:      body,
			signature: "",
			want:      false,
		},
		{
			name:      "missing sha256 prefix",
			secret:    secret,
			body:      body,
			signature: hex.EncodeToString([]byte("badhex")),
			want:      false,
		},
		{
			name:      "empty secret",
			secret:    "",
			body:      body,
			signature: signBody(secret, body),
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateWebhookSignature(tc.secret, tc.body, tc.signature)
			if got != tc.want {
				t.Errorf("ValidateWebhookSignature() = %v, want %v", got, tc.want)
			}
		})
	}
}
