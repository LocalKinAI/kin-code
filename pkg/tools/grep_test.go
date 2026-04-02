package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepFindsPattern(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "sample.txt")
	os.WriteFile(fp, []byte("hello world\nfoo bar\nhello again"), 0644)

	g := &GrepTool{}
	out, err := g.Execute(map[string]any{
		"pattern": "hello",
		"path":    fp,
	})
	if err != nil {
		t.Fatalf("grep error: %v", err)
	}

	if !strings.Contains(out, "hello world") {
		t.Errorf("expected match for 'hello world', got %q", out)
	}
	if !strings.Contains(out, "hello again") {
		t.Errorf("expected match for 'hello again', got %q", out)
	}
}

func TestGrepRegex(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "regex.txt")
	os.WriteFile(fp, []byte("abc123\ndef456\nabc789"), 0644)

	g := &GrepTool{}
	out, err := g.Execute(map[string]any{
		"pattern": "abc\\d+",
		"path":    fp,
	})
	if err != nil {
		t.Fatalf("grep error: %v", err)
	}

	if !strings.Contains(out, "abc123") {
		t.Errorf("expected match for 'abc123', got %q", out)
	}
	if !strings.Contains(out, "abc789") {
		t.Errorf("expected match for 'abc789', got %q", out)
	}
	if strings.Contains(out, "def456") {
		t.Errorf("should not match 'def456', got %q", out)
	}
}

func TestGrepNoMatch(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "nomatch.txt")
	os.WriteFile(fp, []byte("hello world"), 0644)

	g := &GrepTool{}
	out, err := g.Execute(map[string]any{
		"pattern": "zzzzz",
		"path":    fp,
	})
	if err != nil {
		t.Fatalf("grep error: %v", err)
	}

	if !strings.Contains(out, "no matches") {
		t.Errorf("expected 'no matches' message, got %q", out)
	}
}
