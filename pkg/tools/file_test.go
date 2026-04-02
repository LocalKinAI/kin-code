package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileReadWrite(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.txt")

	w := &FileWriteTool{}
	content := "hello world\nline two"
	_, err := w.Execute(map[string]any{
		"file_path": fp,
		"content":   content,
	})
	if err != nil {
		t.Fatalf("write error: %v", err)
	}

	r := &FileReadTool{}
	out, err := r.Execute(map[string]any{
		"file_path": fp,
	})
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected output to contain %q, got %q", "hello world", out)
	}
	if !strings.Contains(out, "line two") {
		t.Errorf("expected output to contain %q, got %q", "line two", out)
	}
}

func TestFileEdit(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit.txt")

	os.WriteFile(fp, []byte("foo bar baz"), 0644)

	e := &FileEditTool{}
	_, err := e.Execute(map[string]any{
		"file_path":  fp,
		"old_string": "bar",
		"new_string": "qux",
	})
	if err != nil {
		t.Fatalf("edit error: %v", err)
	}

	data, _ := os.ReadFile(fp)
	if string(data) != "foo qux baz" {
		t.Errorf("expected %q, got %q", "foo qux baz", string(data))
	}
}

func TestFileEditNotFound(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "edit2.txt")

	os.WriteFile(fp, []byte("hello world"), 0644)

	e := &FileEditTool{}
	_, err := e.Execute(map[string]any{
		"file_path":  fp,
		"old_string": "nonexistent",
		"new_string": "replacement",
	})
	if err == nil {
		t.Fatal("expected error for non-existent old_string, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestFileWriteCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "a", "b", "c", "deep.txt")

	w := &FileWriteTool{}
	_, err := w.Execute(map[string]any{
		"file_path": fp,
		"content":   "deep content",
	})
	if err != nil {
		t.Fatalf("write error: %v", err)
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(data) != "deep content" {
		t.Errorf("expected %q, got %q", "deep content", string(data))
	}
}

func TestFileWriteNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "existing.txt")

	os.WriteFile(fp, []byte("original"), 0644)

	w := &FileWriteTool{}
	_, err := w.Execute(map[string]any{
		"file_path": fp,
		"content":   "new content",
	})
	if err == nil {
		t.Fatal("expected error when overwriting without flag, got nil")
	}
}
