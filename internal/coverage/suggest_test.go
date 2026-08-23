package coverage

import "testing"

// ── the suggestion ───────────────────────────────────────────────────────────

func TestSuggestFromLanguages(t *testing.T) {
	tests := []struct {
		name           string
		languages      map[string]int
		wantLanguage   string
		wantFormat     Format
		wantReportPath string
		wantCommand    bool
	}{
		{
			name:           "go",
			languages:      map[string]int{"Go": 98, "Shell": 2},
			wantLanguage:   "go",
			wantFormat:     FormatGo,
			wantReportPath: "coverage.out",
			wantCommand:    true,
		},
		{
			name:           "typescript",
			languages:      map[string]int{"TypeScript": 80, "CSS": 20},
			wantLanguage:   "typescript",
			wantFormat:     FormatLCOV,
			wantReportPath: "coverage/lcov.info",
			wantCommand:    true,
		},
		{
			name:           "python",
			languages:      map[string]int{"Python": 100},
			wantLanguage:   "python",
			wantFormat:     FormatCobertura,
			wantReportPath: "coverage.xml",
			wantCommand:    true,
		},
		{
			name:           "java",
			languages:      map[string]int{"Java": 95, "XML": 5},
			wantLanguage:   "java",
			wantFormat:     FormatJaCoCo,
			wantReportPath: "target/site/jacoco/jacoco.xml",
			wantCommand:    true,
		},
		{
			// Rust has a conventional report path but no single conventional
			// command, so the command stays empty rather than invented.
			name:           "rust has a path but no guessed command",
			languages:      map[string]int{"Rust": 100},
			wantLanguage:   "rust",
			wantFormat:     FormatLCOV,
			wantReportPath: "lcov.info",
			wantCommand:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestFromLanguages(tt.languages)
			if got.Language != tt.wantLanguage {
				t.Errorf("language = %q, want %q", got.Language, tt.wantLanguage)
			}
			if got.Format != tt.wantFormat {
				t.Errorf("format = %q, want %q", got.Format, tt.wantFormat)
			}
			if got.ReportPath != tt.wantReportPath {
				t.Errorf("report_path = %q, want %q", got.ReportPath, tt.wantReportPath)
			}
			if (got.TestCommand != "") != tt.wantCommand {
				t.Errorf("test_command = %q, want a command: %v", got.TestCommand, tt.wantCommand)
			}
		})
	}
}

// Provider language breakdowns are not consistent about case — GitHub sends
// "JavaScript", and a map keyed on exact case would miss it entirely.
func TestSuggestFromLanguages_IsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"Go", "go", "GO", "gO"} {
		got := SuggestFromLanguages(map[string]int{name: 100})
		if got.Format != FormatGo {
			t.Errorf("SuggestFromLanguages(%q) format = %q, want %q", name, got.Format, FormatGo)
		}
	}
}

// A repository whose heaviest language has no answer must still get a suggestion
// from the next one down: 60% HTML and 40% Go is a Go project with a lot of
// templates, and suggesting the default there would be a worse answer than the
// data supports.
func TestSuggestFromLanguages_FallsThroughToTheNextKnownLanguage(t *testing.T) {
	got := SuggestFromLanguages(map[string]int{"HTML": 60, "Go": 40})

	if got.Language != "go" || got.Format != FormatGo {
		t.Errorf("suggestion = %+v, want the Go suggestion", got)
	}
}

// An unrecognized stack gets LCOV — the most widely produced of the four formats
// this platform parses — and no command, because an invented command is worse
// than a blank the person fills in.
func TestSuggestFromLanguages_UnknownStackGetsTheDefaultAndNoCommand(t *testing.T) {
	for _, languages := range []map[string]int{
		nil,
		{},
		{"Brainfuck": 100},
		{"HTML": 70, "CSS": 30},
	} {
		got := SuggestFromLanguages(languages)
		if got.Format != FormatLCOV {
			t.Errorf("SuggestFromLanguages(%v) format = %q, want %q", languages, got.Format, FormatLCOV)
		}
		if got.TestCommand != "" {
			t.Errorf("SuggestFromLanguages(%v) command = %q, want empty", languages, got.TestCommand)
		}
		// Language is empty so the UI cannot claim it detected something.
		if got.Language != "" {
			t.Errorf("SuggestFromLanguages(%v) language = %q, want empty", languages, got.Language)
		}
	}
}

// A monorepo is the case no heuristic survives, so the only requirement is that
// the answer is deterministic — the same repository must not get a different
// suggestion on the next page load.
func TestSuggestFromLanguages_IsDeterministicOnTies(t *testing.T) {
	languages := map[string]int{"Go": 50, "Python": 50, "TypeScript": 50}

	first := SuggestFromLanguages(languages)
	for i := 0; i < 20; i++ {
		if got := SuggestFromLanguages(languages); got != first {
			t.Fatalf("suggestion changed between calls: %+v then %+v", first, got)
		}
	}
}

// Every suggested format has to be one the ingest endpoint actually accepts, or
// the snippet we hand out is rejected with a 415.
func TestSuggestFromLanguages_AlwaysSuggestsAParseableFormat(t *testing.T) {
	valid := map[Format]bool{}
	for _, format := range Formats() {
		valid[format] = true
	}
	for language := range languageSuggestions {
		got := SuggestFromLanguages(map[string]int{language: 100})
		if !valid[got.Format] {
			t.Errorf("language %q suggests format %q, which the endpoint does not accept", language, got.Format)
		}
		if got.ReportPath == "" {
			t.Errorf("language %q suggests no report path", language)
		}
	}
}

func TestFormats(t *testing.T) {
	got := Formats()
	if len(got) != 4 {
		t.Fatalf("Formats() = %v, want the four the parsers support", got)
	}
	// Ordered so the UI's list is stable across renders.
	want := []Format{FormatGo, FormatLCOV, FormatCobertura, FormatJaCoCo}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Formats()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
