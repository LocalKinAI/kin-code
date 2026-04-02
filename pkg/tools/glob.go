package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LocalKinAI/kin-code/pkg/provider"
)

const maxGlobResults = 500

// GlobTool searches for files using glob patterns.
type GlobTool struct{}

func (g *GlobTool) Name() string { return "glob" }

func (g *GlobTool) Description() string {
	return "Search for files matching a glob pattern. Returns paths sorted by modification time (newest first). Max 500 results."
}

func (g *GlobTool) Def() provider.ToolDef {
	return provider.NewToolDef("glob", g.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern (e.g. '**/*.go', 'src/**/*.ts')",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search in (default: current directory)",
			},
		},
		"required": []string{"pattern"},
	})
}

type fileEntry struct {
	path    string
	modTime int64
}

func (g *GlobTool) Execute(args map[string]any) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	searchPath := "."
	if p, ok := args["path"].(string); ok && p != "" {
		searchPath = p
	}

	var entries []fileEntry

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip hidden directories (except the search path itself).
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && path != searchPath {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		// Match against the pattern.
		relPath, _ := filepath.Rel(searchPath, path)
		matched, err := filepath.Match(pattern, relPath)
		if err != nil {
			// Try matching just the filename for simple patterns.
			matched, _ = filepath.Match(pattern, info.Name())
		}

		// Also support ** by matching the base name.
		if !matched && strings.Contains(pattern, "**") {
			// Strip ** prefix for simple matching.
			simplePattern := strings.TrimPrefix(pattern, "**/")
			matched, _ = filepath.Match(simplePattern, info.Name())
		}

		if matched {
			entries = append(entries, fileEntry{
				path:    path,
				modTime: info.ModTime().UnixNano(),
			})
		}

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("walk directory: %w", err)
	}

	// Sort by modification time (newest first).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime > entries[j].modTime
	})

	if len(entries) > maxGlobResults {
		entries = entries[:maxGlobResults]
	}

	if len(entries) == 0 {
		return "no files matched the pattern", nil
	}

	var paths []string
	for _, e := range entries {
		paths = append(paths, e.path)
	}

	return strings.Join(paths, "\n"), nil
}
