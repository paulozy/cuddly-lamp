package i18n

import (
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantTag     string
		wantDisplay string
		wantErr     bool
	}{
		{name: "empty falls back to english", input: "", wantTag: "en", wantDisplay: "English"},
		{name: "english passes through", input: "en", wantTag: "en", wantDisplay: "English"},
		{name: "pt-br canonicalises case", input: "PT-br", wantTag: "pt-BR", wantDisplay: "Brazilian Portuguese"},
		{name: "spanish", input: "es", wantTag: "es", wantDisplay: "Spanish"},
		{name: "german", input: "de", wantTag: "de", wantDisplay: "German"},
		{name: "japanese", input: "ja", wantTag: "ja", wantDisplay: "Japanese"},
		{name: "simplified chinese", input: "zh-CN", wantTag: "zh-CN", wantDisplay: "Chinese (China)"},
		{name: "invalid tag", input: "not-a-tag-???", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tag, display, err := Resolve(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got tag=%q display=%q", tc.input, tag, display)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tag != tc.wantTag {
				t.Errorf("tag = %q, want %q", tag, tc.wantTag)
			}
			if display != tc.wantDisplay {
				t.Errorf("display = %q, want %q", display, tc.wantDisplay)
			}
		})
	}
}

func TestIsEnglish(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"en", true},
		{"en-US", true},
		{"en-GB", true},
		{"pt-BR", false},
		{"es", false},
		{"invalid-???", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := IsEnglish(tc.input); got != tc.want {
				t.Errorf("IsEnglish(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSupportedLanguages_AllParseable(t *testing.T) {
	for _, tag := range SupportedLanguages {
		if _, _, err := Resolve(tag); err != nil {
			t.Errorf("supported tag %q failed to resolve: %v", tag, err)
		}
	}
}
