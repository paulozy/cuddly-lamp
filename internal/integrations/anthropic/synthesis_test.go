package anthropic

import (
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/ai"
)

func TestBuildSearchSynthesisPrompt_IncludesQueryAndSnippets(t *testing.T) {
	prompt := BuildSearchSynthesisPrompt("how does login work", []ai.SearchSnippet{
		{
			FilePath:  "internal/auth/login.go",
			Content:   "func Login() {}\n",
			Language:  "go",
			StartLine: 10,
			EndLine:   12,
			Score:     0.91,
		},
	})

	for _, want := range []string{
		"how does login work",
		`<snippet file="internal/auth/login.go" lang="go" lines="10-12" score="0.910">`,
		"func Login() {}",
		"untrusted data",
		"Markdown",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\nfull prompt:\n%s", want, prompt)
		}
	}
}

func TestBuildSearchSynthesisPrompt_CapsSnippetCount(t *testing.T) {
	snippets := make([]ai.SearchSnippet, synthesisMaxSnippets+5)
	for i := range snippets {
		snippets[i] = ai.SearchSnippet{
			FilePath:  "f" + string(rune('a'+i)) + ".go",
			Content:   "x",
			Language:  "go",
			StartLine: 1,
			EndLine:   1,
			Score:     0.5,
		}
	}

	prompt := BuildSearchSynthesisPrompt("q", snippets)
	got := strings.Count(prompt, "<snippet ")
	if got != synthesisMaxSnippets {
		t.Errorf("expected %d snippets in prompt, got %d", synthesisMaxSnippets, got)
	}
}

func TestBuildSearchSynthesisPrompt_TruncatesLongSnippet(t *testing.T) {
	var content strings.Builder
	for i := 0; i < synthesisMaxLinesPerSnippet+10; i++ {
		content.WriteString("line\n")
	}

	prompt := BuildSearchSynthesisPrompt("q", []ai.SearchSnippet{{
		FilePath:  "big.go",
		Content:   content.String(),
		Language:  "go",
		StartLine: 1,
		EndLine:   100,
		Score:     0.5,
	}})

	if !strings.Contains(prompt, "(truncated)") {
		t.Error("expected truncation marker for long snippet, but none found")
	}
}

func TestBuildSearchSynthesisPrompt_EscapesSnippetAttributes(t *testing.T) {
	prompt := BuildSearchSynthesisPrompt("q", []ai.SearchSnippet{{
		FilePath:  `evil"path.go`,
		Content:   "ok",
		Language:  `go"x`,
		StartLine: 1,
		EndLine:   1,
		Score:     0.1,
	}})

	if strings.Contains(prompt, `evil"path.go`) {
		t.Error("file path should be HTML-escaped in attributes")
	}
	if !strings.Contains(prompt, `evil&#34;path.go`) {
		t.Errorf("expected escaped file path, got prompt:\n%s", prompt)
	}
}

func TestBuildSearchSynthesisPrompt_Deterministic(t *testing.T) {
	snippets := []ai.SearchSnippet{
		{FilePath: "a.go", Content: "a", Language: "go", StartLine: 1, EndLine: 1, Score: 0.5},
		{FilePath: "b.go", Content: "b", Language: "go", StartLine: 2, EndLine: 2, Score: 0.4},
	}
	a := BuildSearchSynthesisPrompt("q", snippets)
	b := BuildSearchSynthesisPrompt("q", snippets)
	if a != b {
		t.Error("prompt builder must be deterministic for the same input")
	}
}

func TestTruncateLines(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		max     int
		want    string
		wantLen int
	}{
		{"short", "a\nb\nc", 5, "a\nb\nc", 5},
		{"exact", "a\nb\nc", 3, "a\nb\nc", 5},
		{"truncate", "a\nb\nc\nd", 2, "a\nb\n... (truncated)", 0},
		{"zero max", "a\nb", 0, "a\nb", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateLines(tc.input, tc.max)
			if tc.wantLen > 0 && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if tc.wantLen == 0 && !strings.HasSuffix(got, "(truncated)") {
				t.Errorf("expected truncation suffix, got %q", got)
			}
		})
	}
}
