package tools

import (
	"strings"
	"testing"
)

func newTestSkillStore(t *testing.T) *SkillStore {
	dir := t.TempDir()
	return &SkillStore{dir: dir}
}

func TestSkillSaveLoad(t *testing.T) {
	s := newTestSkillStore(t)

	err := s.Save("greet", "# Greeting Skill\nSay hello to the user.")
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	content, err := s.Load("greet")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !strings.Contains(content, "Greeting Skill") {
		t.Errorf("expected content to contain %q, got %q", "Greeting Skill", content)
	}
}

func TestSkillList(t *testing.T) {
	s := newTestSkillStore(t)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := s.Save(name, "content for "+name); err != nil {
			t.Fatalf("save error for %q: %v", name, err)
		}
	}

	names, err := s.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}

	if len(names) != 3 {
		t.Errorf("expected 3 skills, got %d", len(names))
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !found[want] {
			t.Errorf("expected skill %q in list", want)
		}
	}
}

func TestSkillNotFound(t *testing.T) {
	s := newTestSkillStore(t)

	_, err := s.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill, got nil")
	}
}
