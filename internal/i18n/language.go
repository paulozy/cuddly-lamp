// Package i18n centralises BCP 47 language handling for AI output configuration.
package i18n

import (
	"fmt"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// DefaultLanguage is the canonical fallback used whenever no preference is set.
// It mirrors the database default on organization_configs.output_language.
const DefaultLanguage = "en"

// SupportedLanguages is the curated set we test against and surface in admin UIs.
// The backend itself accepts any well-formed BCP 47 tag — this list is advisory.
var SupportedLanguages = []string{
	"en",
	"pt-BR",
	"es",
	"fr",
	"de",
	"it",
	"ja",
	"zh-CN",
}

// Resolve normalises a user-supplied BCP 47 tag and returns:
//   - canonical: the canonicalised tag (e.g. "PT-br" -> "pt-BR"). When the
//     input is empty, returns DefaultLanguage.
//   - displayName: the language's name in English, suitable for a system
//     prompt (e.g. "Brazilian Portuguese").
//   - err: a non-nil error if the input is non-empty and not a valid BCP 47 tag.
func Resolve(tag string) (canonical, displayName string, err error) {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return DefaultLanguage, "English", nil
	}
	parsed, err := language.Parse(trimmed)
	if err != nil {
		return "", "", fmt.Errorf("invalid bcp 47 language tag %q: %w", tag, err)
	}
	canonical = parsed.String()
	displayName = display.English.Tags().Name(parsed)
	if displayName == "" {
		displayName = canonical
	}
	return canonical, displayName, nil
}

// IsEnglish returns true if the tag resolves to the English root language,
// regardless of region. Callers use it to skip language-specific work when
// the output is already in our canonical English.
func IsEnglish(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return true
	}
	parsed, err := language.Parse(trimmed)
	if err != nil {
		return false
	}
	base, _ := parsed.Base()
	return base.String() == "en"
}
