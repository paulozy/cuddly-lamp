package anthropic

import (
	"fmt"

	"github.com/paulozy/idp-with-ai-backend/internal/i18n"
)

// BuildSystemPrompt returns the Anthropic `System` content used to localise
// Claude's prose output. It deliberately returns an empty string when the
// requested language is empty or already English so the caller can omit the
// System parameter entirely (preserving today's behaviour and saving tokens).
//
// Enum-like fields stay in canonical English regardless of the requested
// language so downstream consumers (UI filters, business logic) keep working.
func BuildSystemPrompt(language string) string {
	if i18n.IsEnglish(language) {
		return ""
	}
	_, displayName, err := i18n.Resolve(language)
	if err != nil || displayName == "" {
		return ""
	}
	return fmt.Sprintf(
		"Always respond in %s. "+
			"Translate only human-readable prose: summary, title, description, "+
			"suggestion, and any Markdown body. "+
			"Keep these enum-like fields in canonical English exactly as documented: "+
			"severity, category, pattern, cwe_id, owasp_category, debt_category. "+
			"Do not translate JSON keys or field names.",
		displayName,
	)
}
