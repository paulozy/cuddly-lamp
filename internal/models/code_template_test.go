package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodeTemplate_MarshalJSON_PendingIncludesZeroNumericFields(t *testing.T) {
	tmpl := CodeTemplate{
		ID:             "tmpl-123",
		OrganizationID: "org-1",
		Prompt:         "scaffold a service",
		Status:         TemplateStatusPending,
	}

	out, err := json.Marshal(tmpl)
	if err != nil {
		t.Fatalf("marshal CodeTemplate: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, `"tokens_used":0`) {
		t.Errorf("expected tokens_used:0 to be present in JSON for pending template; got %s", got)
	}
	if !strings.Contains(got, `"processing_ms":0`) {
		t.Errorf("expected processing_ms:0 to be present in JSON for pending template; got %s", got)
	}
}
