package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobFindsFiles(t *testing.T) {
	dir := t.TempDir()

	// Create some .txt files.
	for _, name := range []string{"a.txt", "b.txt", "c.go"} {
		os.WriteFile(filepath.Join(dir, name), []byte("content"), 0644)
	}

	g := &GlobTool{}
	out, err := g.Execute(map[string]any{
		"pattern": "*.txt",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}

	if !strings.Contains(out, "a.txt") {
		t.Errorf("expected output to contain a.txt, got %q", out)
	}
	if !strings.Contains(out, "b.txt") {
		t.Errorf("expected output to contain b.txt, got %q", out)
	}
	if strings.Contains(out, "c.go") {
		t.Errorf("output should not contain c.go, got %q", out)
	}
}

func TestGlobNoMatch(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "file.go"), []byte("content"), 0644)

	g := &GlobTool{}
	out, err := g.Execute(map[string]any{
		"pattern": "*.xyz",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}

	if !strings.Contains(out, "no files matched") {
		t.Errorf("expected 'no files matched' message, got %q", out)
	}
}
