package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

const maxGrepResults = 250

// GrepTool searches file contents using Go regexp.
type GrepTool struct{}

func (g *GrepTool) Name() string { return "grep" }

func (g *GrepTool) Description() string {
	return "Search file contents using regular expressions. Returns matching lines with file path and line number."
}

func (g *GrepTool) Def() provider.ToolDef {
	return provider.NewToolDef("grep", g.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory to search in (default: current directory)",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g. '*.go', '*.ts')",
			},
			"context": map[string]any{
				"type":        "integer",
				"description": "Number of context lines before and after match (default 0)",
			},
			"case_insensitive": map[string]any{
				"type":        "boolean",
				"description": "Case insensitive search (default false)",
			},
		},
		"required": []string{"pattern"},
	})
}

type grepMatch struct {
	file    string
	lineNum int
	line    string
}

func (g *GrepTool) Execute(args map[string]any) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	caseInsensitive := false
	if ci, ok := args["case_insensitive"].(bool); ok {
		caseInsensitive = ci
	}

	if caseInsensitive {
		pattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	searchPath := "."
	if p, ok := args["path"].(string); ok && p != "" {
		searchPath = p
	}

	globPattern := ""
	if gl, ok := args["glob"].(string); ok {
		globPattern = gl
	}

	contextLines := 0
	if c, ok := args["context"].(float64); ok && c > 0 {
		contextLines = int(c)
	}

	info, err := os.Stat(searchPath)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}

	var matches []grepMatch

	if !info.IsDir() {
		// Search single file.
		m, err := searchFile(searchPath, re)
		if err != nil {
			return "", err
		}
		matches = append(matches, m...)
	} else {
		// Walk directory.
		_ = filepath.Walk(searchPath, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fi.IsDir() {
				if strings.HasPrefix(fi.Name(), ".") && path != searchPath {
					return filepath.SkipDir
				}
				// Skip common non-code directories.
				switch fi.Name() {
				case "node_modules", "vendor", ".git", "__pycache__", "dist", "build":
					return filepath.SkipDir
				}
				return nil
			}
			if fi.Size() > 1024*1024 { // skip files > 1MB
				return nil
			}
			if globPattern != "" {
				matched, _ := filepath.Match(globPattern, fi.Name())
				if !matched {
					return nil
				}
			}
			m, _ := searchFile(path, re)
			matches = append(matches, m...)
			if len(matches) > maxGrepResults*2 {
				return fmt.Errorf("too many results")
			}
			return nil
		})
	}

	if len(matches) == 0 {
		return "no matches found", nil
	}

	if len(matches) > maxGrepResults {
		matches = matches[:maxGrepResults]
	}

	// Format output, optionally with context.
	_ = contextLines // TODO: implement context lines for v2
	var sb strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&sb, "%s:%d: %s\n", m.file, m.lineNum, m.line)
	}

	result := sb.String()
	if len(matches) == maxGrepResults {
		result += fmt.Sprintf("\n... (results capped at %d matches)", maxGrepResults)
	}

	return result, nil
}

func searchFile(path string, re *regexp.Regexp) ([]grepMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []grepMatch
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, grepMatch{
				file:    path,
				lineNum: lineNum,
				line:    line,
			})
		}
	}

	return matches, nil
}
