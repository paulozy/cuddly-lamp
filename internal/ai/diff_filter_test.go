package ai

import (
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
)

func TestFilterAndMapPRFiles(t *testing.T) {
	tests := []struct {
		name string
		file github.PRFile
		want []ChangedFile
	}{
		{
			name: "normal file passthrough",
			file: github.PRFile{Filename: "src/app.go", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new"},
			want: []ChangedFile{{Path: "src/app.go", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new"}},
		},
		{
			name: "deny by extension",
			file: github.PRFile{Filename: "web/app.min.js", Status: "modified", Patch: "+min"},
		},
		{
			name: "deny by filename",
			file: github.PRFile{Filename: "go.sum", Status: "modified", Patch: "+sum"},
		},
		{
			name: "deny by path prefix",
			file: github.PRFile{Filename: "vendor/lib/lib.go", Status: "modified", Patch: "+vendored"},
		},
		{
			name: "empty patch skipped",
			file: github.PRFile{Filename: "assets/logo.png", Status: "modified"},
		},
		{
			name: "removed file patch collapsed",
			file: github.PRFile{Filename: "old/deleted.go", Status: "removed", Patch: "@@ -1 +0 @@\n-old"},
			want: []ChangedFile{{Path: "old/deleted.go", Status: "removed"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterAndMapPRFiles([]github.PRFile{tt.file})
			if len(got) != len(tt.want) {
				t.Fatalf("FilterAndMapPRFiles returned %d files, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("FilterAndMapPRFiles[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBudgetChangedFiles(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		kept, truncated := BudgetChangedFiles(nil)
		if len(kept) != 0 || len(truncated) != 0 {
			t.Fatalf("BudgetChangedFiles(nil) = kept %+v truncated %+v, want empty", kept, truncated)
		}
	})

	t.Run("all files fit", func(t *testing.T) {
		files := []ChangedFile{
			{Path: "small.go", Patch: strings.Repeat("a", 40)},
			{Path: "large.go", Patch: strings.Repeat("b", 80)},
		}

		kept, truncated := BudgetChangedFiles(files)
		if len(kept) != 2 || len(truncated) != 0 {
			t.Fatalf("BudgetChangedFiles kept %d truncated %d, want 2/0", len(kept), len(truncated))
		}
		if kept[0].Path != "large.go" || kept[1].Path != "small.go" {
			t.Fatalf("BudgetChangedFiles should keep largest first, got %+v", kept)
		}
	})

	t.Run("budget exhausted", func(t *testing.T) {
		files := []ChangedFile{
			{Path: "largest.go", Patch: strings.Repeat("a", 120_000)},
			{Path: "middle.go", Patch: strings.Repeat("b", 80_000)},
			{Path: "small.go", Patch: strings.Repeat("c", 40_004)},
		}

		kept, truncated := BudgetChangedFiles(files)
		if len(kept) != 2 || kept[0].Path != "largest.go" || kept[1].Path != "middle.go" {
			t.Fatalf("BudgetChangedFiles kept %+v, want largest and middle", kept)
		}
		if len(truncated) != 1 || truncated[0] != "small.go" {
			t.Fatalf("BudgetChangedFiles truncated %+v, want small.go", truncated)
		}
	})

	t.Run("single oversized file", func(t *testing.T) {
		files := []ChangedFile{
			{Path: "huge.go", Patch: strings.Repeat("a", 200_004)},
		}

		kept, truncated := BudgetChangedFiles(files)
		if len(kept) != 0 {
			t.Fatalf("BudgetChangedFiles kept %+v, want none", kept)
		}
		if len(truncated) != 1 || truncated[0] != "huge.go" {
			t.Fatalf("BudgetChangedFiles truncated %+v, want huge.go", truncated)
		}
	})
}
