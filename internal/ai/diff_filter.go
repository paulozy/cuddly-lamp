package ai

import (
	"sort"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
)

const diffTokenBudget = 50_000

var denyExtensions = []string{
	".lock",
	".sum",
	".min.js",
	".min.css",
	".map",
	".pb.go",
	"_generated.go",
	".gen.go",
}

var denyFilenames = map[string]struct{}{
	"package-lock.json": {},
	"yarn.lock":         {},
	"pnpm-lock.yaml":    {},
	"go.sum":            {},
	"Cargo.lock":        {},
	"Gemfile.lock":      {},
	"composer.lock":     {},
	"poetry.lock":       {},
}

var denyPathPrefixes = []string{
	"vendor/",
	"node_modules/",
	"dist/",
	"build/",
}

// FilterAndMapPRFiles removes noisy or unavailable PR diffs and converts them
// to the provider-neutral ChangedFile shape.
func FilterAndMapPRFiles(files []github.PRFile) []ChangedFile {
	changed := make([]ChangedFile, 0, len(files))
	for _, file := range files {
		if shouldDenyPRFile(file.Filename) {
			continue
		}

		if file.Status == "removed" {
			changed = append(changed, ChangedFile{
				Path:   file.Filename,
				Status: file.Status,
			})
			continue
		}

		if strings.TrimSpace(file.Patch) == "" {
			continue
		}

		changed = append(changed, ChangedFile{
			Path:   file.Filename,
			Patch:  file.Patch,
			Status: file.Status,
		})
	}
	return changed
}

// BudgetChangedFiles keeps the largest diffs first until the diff token budget
// is exhausted, returning paths for files dropped from the request.
func BudgetChangedFiles(files []ChangedFile) ([]ChangedFile, []string) {
	if len(files) == 0 {
		return nil, nil
	}

	sorted := append([]ChangedFile(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].Patch) > len(sorted[j].Patch)
	})

	kept := make([]ChangedFile, 0, len(sorted))
	truncated := make([]string, 0)
	used := 0
	for _, file := range sorted {
		estimatedTokens := len(file.Patch) / 4
		if used+estimatedTokens > diffTokenBudget {
			truncated = append(truncated, file.Path)
			continue
		}
		used += estimatedTokens
		kept = append(kept, file)
	}

	return kept, truncated
}

func shouldDenyPRFile(path string) bool {
	base := path
	if slash := strings.LastIndex(path, "/"); slash >= 0 {
		base = path[slash+1:]
	}
	if _, denied := denyFilenames[base]; denied {
		return true
	}

	for _, prefix := range denyPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	for _, ext := range denyExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
