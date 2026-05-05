package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SubAgentPersona is a named, persona-specific configuration for a
// sub-agent. Loaded from .md files at ~/.kincode/agents/<name>.md
// or ~/.localkin/agents/<name>.md (family-shared).
//
// File format mirrors Claude Code's named-subagent convention:
//
//	---
//	name: "code-reviewer"
//	description: "Senior code reviewer — flags bugs, style, security issues."
//	tools: ["bash", "file_read", "glob", "grep"]   # optional restrict; empty = all
//	model: "claude-sonnet-4-6"                      # optional override; empty = parent's
//	---
//	You are a senior code reviewer. You read the diff, then for each
//	change you assess: correctness, security, performance, style.
//	You flag exactly what's wrong, where, and how to fix it.
//	You never apologize.
//
// agent_spawn picks subagents two ways:
//   1. Explicit: agent_spawn(agent="code-reviewer", task="...")
//   2. Implicit: agent_spawn(task="...") with no agent — uses
//      parent's system prompt (the legacy flow, unchanged).
//
// `description` is what the parent model sees in the agent_spawn
// tool description, so make it actionable: "Reviews code for X" not
// "A code reviewer". The agent uses it to decide whether to delegate.
type SubAgentPersona struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Tools        []string `yaml:"tools"`
	Model        string   `yaml:"model"`
	SystemPrompt string   `yaml:"-"` // body of the .md file
	Path         string   `yaml:"-"` // file path for diagnostics
}

// LoadSubAgentPersona reads ~/.kincode/agents/<name>.md (preferred)
// or ~/.localkin/agents/<name>.md (family-shared fallback) and
// returns the parsed persona. Returns nil + error if neither exists
// or the file is malformed.
//
// User-level overrides family-level by name — kincode-specific
// "code-reviewer" wins over a shared family "code-reviewer".
func LoadSubAgentPersona(name string) (*SubAgentPersona, error) {
	if name == "" {
		return nil, fmt.Errorf("persona name is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	candidates := []string{
		filepath.Join(home, ".kincode", "agents", name+".md"),
		filepath.Join(home, ".localkin", "agents", name+".md"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parsePersonaFile(path, data)
	}
	return nil, fmt.Errorf("persona %q not found in ~/.kincode/agents/ or ~/.localkin/agents/", name)
}

// ListSubAgentPersonas walks both persona directories and returns
// every parseable persona, deduplicated by name (user-level wins).
// Used at boot to build the agent_spawn tool description so the
// parent model knows which subagents are available without having
// to discover them via filesystem queries.
func ListSubAgentPersonas() []SubAgentPersona {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dirs := []string{
		filepath.Join(home, ".kincode", "agents"),
		filepath.Join(home, ".localkin", "agents"),
	}
	seen := map[string]bool{}
	var out []SubAgentPersona
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			p, err := parsePersonaFile(path, data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[persona] skipping %s: %v\n", path, err)
				continue
			}
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			out = append(out, *p)
		}
	}
	// Sort alphabetically for stable description output.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// parsePersonaFile splits the .md into YAML frontmatter + body and
// constructs a SubAgentPersona. Frontmatter must have name +
// description; body becomes the system prompt verbatim (trimmed).
func parsePersonaFile(path string, data []byte) (*SubAgentPersona, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return nil, fmt.Errorf("missing leading --- frontmatter")
	}
	rest := s[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, fmt.Errorf("missing closing ---")
	}
	yamlPart := rest[:idx]
	bodyStart := idx + 4
	if bodyStart < len(rest) && rest[bodyStart] == '\n' {
		bodyStart++
	}
	body := ""
	if bodyStart <= len(rest) {
		body = strings.TrimSpace(rest[bodyStart:])
	}
	var p SubAgentPersona
	if err := yaml.Unmarshal([]byte(yamlPart), &p); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("name is required in frontmatter")
	}
	if p.Description == "" {
		return nil, fmt.Errorf("description is required in frontmatter")
	}
	p.SystemPrompt = body
	p.Path = path
	return &p, nil
}
