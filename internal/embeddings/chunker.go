package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	defaultChunkLines   = 120
	defaultChunkOverlap = 20
	maxFileBytes        = 512 * 1024
)

type CodeChunk struct {
	FilePath    string
	Content     string
	ContentHash string
	Language    string
	StartLine   int
	EndLine     int
	Tokens      int
}

func CollectRepositoryChunks(ctx context.Context, repoURL, githubToken, branch string) ([]CodeChunk, error) {
	dir, err := os.MkdirTemp("", "embeddings-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	opts := &git.CloneOptions{
		URL:   repoURL,
		Depth: 1,
	}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}
	if githubToken != "" {
		opts.Auth = &githttp.BasicAuth{Username: "x-token", Password: githubToken}
	}

	if _, err := git.PlainCloneContext(ctx, dir, false, opts); err != nil {
		return nil, fmt.Errorf("clone repo: %w", err)
	}

	var chunks []CodeChunk
	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if shouldSkipPath(path) && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipPath(path) || info.Size() == 0 || info.Size() > maxFileBytes {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil || looksBinary(content) {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		chunks = append(chunks, chunkFile(filepath.ToSlash(rel), string(content))...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repository: %w", err)
	}

	return chunks, nil
}

func chunkFile(filePath, content string) []CodeChunk {
	lines := strings.Split(content, "\n")
	language := detectLanguage(filePath)
	var chunks []CodeChunk

	for start := 0; start < len(lines); {
		end := start + defaultChunkLines
		if end > len(lines) {
			end = len(lines)
		}
		chunkText := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if chunkText != "" {
			sum := sha256.Sum256([]byte(filePath + "\n" + chunkText))
			chunks = append(chunks, CodeChunk{
				FilePath:    filePath,
				Content:     chunkText,
				ContentHash: hex.EncodeToString(sum[:]),
				Language:    language,
				StartLine:   start + 1,
				EndLine:     end,
				Tokens:      estimateTokens(chunkText),
			})
		}
		if end == len(lines) {
			break
		}
		start = end - defaultChunkOverlap
		if start < 0 {
			start = end
		}
	}

	return chunks
}

func shouldSkipPath(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return true
	}
	skipParts := map[string]bool{
		"node_modules": true, "vendor": true, "dist": true, "build": true,
		"target": true, ".git": true, ".gradle": true, ".idea": true,
		"coverage": true, "__pycache__": true,
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if skipParts[part] {
			return true
		}
	}
	ext := strings.ToLower(filepath.Ext(path))
	skipExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".rar": true,
		".exe": true, ".dll": true, ".so": true, ".o": true, ".class": true,
		".jar": true, ".lock": true, ".sum": true,
	}
	return skipExts[ext] || strings.HasSuffix(path, ".min.js") || strings.HasSuffix(path, ".min.css")
}

func looksBinary(content []byte) bool {
	for i, b := range content {
		if i > 8000 {
			break
		}
		if b == 0 {
			return true
		}
	}
	return false
}

func detectLanguage(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".go":
		return "Go"
	case ".py":
		return "Python"
	case ".js", ".mjs":
		return "JavaScript"
	case ".ts":
		return "TypeScript"
	case ".tsx":
		return "TSX"
	case ".jsx":
		return "JSX"
	case ".java":
		return "Java"
	case ".c", ".h":
		return "C"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "C++"
	case ".cs":
		return "C#"
	case ".rb":
		return "Ruby"
	case ".rs":
		return "Rust"
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
	case ".yaml", ".yml":
		return "YAML"
	case ".toml":
		return "TOML"
	case ".md", ".mdx":
		return "Markdown"
	default:
		return ""
	}
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) / 4) + 1
}
