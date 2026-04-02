// Package tools provides the skill template store for kin-code.
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillStore manages reusable prompt templates stored as .md files.
type SkillStore struct {
	dir string
}

// NewSkillStore creates a SkillStore rooted at ~/.kin-code/skills/.
func NewSkillStore() *SkillStore {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".kin-code", "skills")
	_ = os.MkdirAll(dir, 0755)
	return &SkillStore{dir: dir}
}

// List returns the names of all available skills (without .md extension).
func (s *SkillStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".md") {
			names = append(names, strings.TrimSuffix(name, ".md"))
		}
	}
	return names, nil
}

// Load reads the content of a skill template by name.
func (s *SkillStore) Load(name string) (string, error) {
	path := filepath.Join(s.dir, name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("skill %q not found: %w", name, err)
	}
	return string(data), nil
}

// Save writes a new skill template.
func (s *SkillStore) Save(name, content string) error {
	_ = os.MkdirAll(s.dir, 0755)
	path := filepath.Join(s.dir, name+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("save skill %q: %w", name, err)
	}
	return nil
}
