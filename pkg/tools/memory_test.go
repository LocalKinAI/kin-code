package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

func newTestMemory(t *testing.T) *MemoryTool {
	dir := t.TempDir()
	return &MemoryTool{
		path: filepath.Join(dir, "memory.json"),
	}
}

func TestMemoryWriteRead(t *testing.T) {
	m := newTestMemory(t)

	// Write a value.
	_, err := m.Execute(map[string]any{
		"action": "write",
		"key":    "name",
		"value":  "Jacky",
	})
	if err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Read it back.
	out, err := m.Execute(map[string]any{
		"action": "read",
		"key":    "name",
	})
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if out != "Jacky" {
		t.Errorf("expected %q, got %q", "Jacky", out)
	}
}

func TestMemoryList(t *testing.T) {
	m := newTestMemory(t)

	// Write multiple keys.
	for _, kv := range []struct{ k, v string }{
		{"color", "blue"},
		{"food", "pizza"},
		{"lang", "Go"},
	} {
		_, err := m.Execute(map[string]any{
			"action": "write",
			"key":    kv.k,
			"value":  kv.v,
		})
		if err != nil {
			t.Fatalf("write error for %q: %v", kv.k, err)
		}
	}

	// List all.
	out, err := m.Execute(map[string]any{
		"action": "list",
	})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}

	for _, key := range []string{"color", "food", "lang"} {
		if !strings.Contains(out, key) {
			t.Errorf("list output should contain %q, got %q", key, out)
		}
	}
}

func TestMemorySearch(t *testing.T) {
	m := newTestMemory(t)

	m.Execute(map[string]any{"action": "write", "key": "project", "value": "kin-code"})
	m.Execute(map[string]any{"action": "write", "key": "hobby", "value": "coding"})
	m.Execute(map[string]any{"action": "write", "key": "food", "value": "tacos"})

	// Search for "cod" should match "kin-code" and "coding".
	out, err := m.Execute(map[string]any{
		"action": "search",
		"value":  "cod",
	})
	if err != nil {
		t.Fatalf("search error: %v", err)
	}

	if !strings.Contains(out, "kin-code") {
		t.Errorf("search should find 'kin-code', got %q", out)
	}
	if !strings.Contains(out, "coding") {
		t.Errorf("search should find 'coding', got %q", out)
	}
	if strings.Contains(out, "tacos") {
		t.Errorf("search should NOT find 'tacos', got %q", out)
	}
}

func TestMemoryReadNotFound(t *testing.T) {
	m := newTestMemory(t)

	out, err := m.Execute(map[string]any{
		"action": "read",
		"key":    "nonexistent",
	})
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' message, got %q", out)
	}
}
