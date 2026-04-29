package metrics

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
)

// Calculate clones a repository and computes code metrics (lines of code, complexity)
// It returns the metrics or an error if cloning/analysis fails.
// Note: Test coverage is not calculated as it is a CI artifact, not a git artifact.
func Calculate(ctx context.Context, repoURL, githubToken string) (*ai.CodeMetrics, error) {
	// Create temporary directory for clone
	dir, err := os.MkdirTemp("", "analysis-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// Clone repository with shallow depth (single commit, no history)
	_, err = git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:   repoURL,
		Depth: 1, // shallow clone to minimize network/disk usage
		Auth: &githttp.BasicAuth{
			Username: "x-token",
			Password: githubToken,
		},
		// RecurseSubmodules is git.NoRecurseSubmodules by default (safe default)
	})
	if err != nil {
		return nil, fmt.Errorf("clone repo: %w", err)
	}

	// Walk directory and count lines
	var totalLines, codeLines, blankLines int32
	var complexity int32 = 0

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		// Skip non-code files
		if shouldSkipFile(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}

		// Count lines: blank, code, and estimate complexity
		lines, blank, code := countLines(content)
		totalLines += lines
		blankLines += blank
		codeLines += code
		complexity += estimateComplexity(content)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	return &ai.CodeMetrics{
		LinesOfCode:          totalLines,
		CyclomaticComplexity: complexity,
		TestCoverage:         0, // Not calculated from git — it's a CI artifact
		CodeDuplication:      0, // Not implemented
		MaintainabilityIndex: 0, // Not implemented
	}, nil
}

// shouldSkipFile returns true for files/dirs that are not source code
func shouldSkipFile(path string) bool {
	skipPatterns := []string{
		".git", ".gitignore", ".gitmodules",
		".env", ".env.example", ".env.local",
		"node_modules", "vendor", ".venv", "venv",
		"build", "dist", "target", ".gradle", ".idea",
		".DS_Store", "Thumbs.db",
		".lock", ".sum", ".mod",
		"package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	}

	// Check if any skip pattern matches the path
	for _, pattern := range skipPatterns {
		if strings.Contains(path, string(os.PathSeparator)+pattern+string(os.PathSeparator)) ||
			strings.HasPrefix(filepath.Base(path), ".") ||
			strings.HasSuffix(path, ".min.js") || strings.HasSuffix(path, ".min.css") {
			return true
		}
	}

	// Skip binary files and common non-source extensions
	ext := filepath.Ext(path)
	skipExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".o": true,
		".pyc": true, ".pyo": true, ".class": true, ".jar": true,
		".zip": true, ".tar": true, ".gz": true, ".rar": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
		".pdf": true, ".doc": true, ".xlsx": true, ".mp3": true,
	}

	return skipExts[ext]
}

// countLines counts total, blank, and code lines in content
func countLines(content []byte) (total, blank, code int32) {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		total++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			blank++
		} else if !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "#") {
			code++
		}
	}
	return
}

// detectLanguage returns a language hint for scc based on file extension
func detectLanguage(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".go":
		return "Go"
	case ".py":
		return "Python"
	case ".js", ".mjs":
		return "JavaScript"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".jsx":
		return "JSX"
	case ".java":
		return "Java"
	case ".c":
		return "C"
	case ".cpp", ".cc", ".cxx", ".h":
		return "C++"
	case ".cs":
		return "C#"
	case ".rb":
		return "Ruby"
	case ".sh", ".bash":
		return "Shell"
	case ".sql":
		return "SQL"
	case ".html", ".htm":
		return "HTML"
	case ".css":
		return "CSS"
	case ".json":
		return "JSON"
	case ".xml":
		return "XML"
	case ".yaml", ".yml":
		return "YAML"
	case ".toml":
		return "TOML"
	default:
		return ""
	}
}

// estimateComplexity counts branch keywords to estimate cyclomatic complexity
// This is a heuristic and not the formal McCabe complexity, but works across languages
func estimateComplexity(content []byte) int32 {
	text := string(content)
	keywords := []string{
		"if ", "else ", "else if ", "switch ", "case ", "for ", "while ", "do ",
		"catch ", "try ", "&&", "||", "?", ":", "goto ", "return ",
	}

	var count int32
	for _, kw := range keywords {
		for i := 0; i < len(text); i++ {
			if i+len(kw) <= len(text) && text[i:i+len(kw)] == kw {
				count++
				i += len(kw) - 1
			}
		}
	}

	// Simple heuristic: divide by 4 to avoid over-counting
	if count > 0 {
		count = (count + 3) / 4
	}
	return count
}
