package anthropic

import (
	"testing"
)

func TestClient_Provider(t *testing.T) {
	client := NewClient("test-key")
	if client.Provider() != "anthropic" {
		t.Errorf("Expected provider 'anthropic', got '%s'", client.Provider())
	}
}
