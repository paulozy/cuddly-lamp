package anthropic

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_EmptyForEnglish(t *testing.T) {
	cases := []string{"", "en", "en-US", "en-GB"}
	for _, c := range cases {
		if got := BuildSystemPrompt(c); got != "" {
			t.Errorf("BuildSystemPrompt(%q) = %q, want empty string", c, got)
		}
	}
}

func TestBuildSystemPrompt_PortugueseBrazil(t *testing.T) {
	got := BuildSystemPrompt("pt-BR")
	if got == "" {
		t.Fatal("expected non-empty system prompt for pt-BR")
	}
	if !strings.Contains(got, "Brazilian Portuguese") {
		t.Errorf("prompt missing display name: %q", got)
	}
}

func TestBuildSystemPrompt_PreservesEnumFields(t *testing.T) {
	got := BuildSystemPrompt("es")
	for _, field := range []string{"severity", "category", "pattern", "cwe_id", "owasp_category", "debt_category"} {
		if !strings.Contains(got, field) {
			t.Errorf("prompt missing protected enum field %q\nfull prompt: %s", field, got)
		}
	}
}

func TestBuildSystemPrompt_InvalidTagReturnsEmpty(t *testing.T) {
	if got := BuildSystemPrompt("not-a-real-tag-???"); got != "" {
		t.Errorf("invalid tag should return empty prompt, got %q", got)
	}
}
