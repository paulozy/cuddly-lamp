package handlers

import (
	"encoding/json"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

func TestAnalyzeRepositoryRequest(t *testing.T) {
	req := models.AnalyzeRepositoryRequest{
		Type:   "code_review",
		Branch: "main",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded models.AnalyzeRepositoryRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Type != "code_review" || decoded.Branch != "main" {
		t.Error("Request fields not preserved")
	}
}
