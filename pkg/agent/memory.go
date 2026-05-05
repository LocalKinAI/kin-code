package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadProjectMemory walks upward from `startDir` looking for a project
// memory file (KINCODE.md, falling back to CLAUDE.md for cross-tool
// portability) and returns the first one's contents. Walks at most
// to the user's home dir — never escapes into system territory or
// /. Returns empty string if nothing found at any level.
//
// Why two filenames: kincode is a fork of the Claude Code conventions,
// and many users will already have CLAUDE.md sitting in their repos
// from prior Claude usage. Honoring CLAUDE.md as a fallback means
// users who switch from Claude Code to kincode get their existing
// project context immediately. KINCODE.md takes priority when both
// exist so kincode-specific overrides work.
//
// Format: any plain Markdown. The file is prepended verbatim to the
// agent's system prompt. Common patterns:
//
//	# Project: kincode
//	Go binary, no external deps. Tests in pkg/*/_test.go.
//	Run: go test ./...
//
//	## Conventions
//	- Use stdlib HTTP, not net/http/httptest for live server tests
//	- Provider impls live in pkg/provider/
//	- Soul format mirrors kinclaw kernel — same .soul.md drives both
//
//	## Don't
//	- Add cgo dependencies
//	- Touch examples/ without me asking
func LoadProjectMemory(startDir string) string {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	dir := startDir
	candidates := []string{"KINCODE.md", "CLAUDE.md"}

	// Walk up at most ~10 levels so a misconfigured cwd doesn't loop
	// forever; in practice nobody nests project repos that deep.
	for i := 0; i < 10; i++ {
		for _, name := range candidates {
			path := filepath.Join(dir, name)
			if data, err := os.ReadFile(path); err == nil {
				return strings.TrimSpace(string(data))
			}
		}
		// Stop if we hit home or root.
		parent := filepath.Dir(dir)
		if parent == dir || (home != "" && dir == home) {
			break
		}
		dir = parent
	}
	return ""
}

// FormatProjectMemory wraps LoadProjectMemory's content in a clear
// delimiter so the agent can distinguish project context from soul
// rules. Empty input returns empty output (caller can concatenate
// unconditionally).
func FormatProjectMemory(content string) string {
	if content == "" {
		return ""
	}
	return "\n\n## Project memory (KINCODE.md)\n\n" + content
}
