package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

// MemoryTool provides persistent key-value storage across sessions.
type MemoryTool struct {
	mu   sync.Mutex
	path string
}

func (m *MemoryTool) Name() string { return "memory" }

func (m *MemoryTool) Description() string {
	return "Persistent memory across sessions. Store/retrieve key-value pairs in ~/.kincode/memory.json."
}

func (m *MemoryTool) Def() provider.ToolDef {
	return provider.NewToolDef("memory", m.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: read, write, list, or search",
				"enum":        []string{"read", "write", "list", "search"},
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Key for read/write operations",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value for write operations, or search term for search",
			},
		},
		"required": []string{"action"},
	})
}

func (m *MemoryTool) memoryPath() string {
	if m.path != "" {
		return m.path
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".kincode", "memory.json")
}

func (m *MemoryTool) load() (map[string]string, error) {
	data, err := os.ReadFile(m.memoryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("read memory: %w", err)
	}
	var store map[string]string
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse memory: %w", err)
	}
	return store, nil
}

func (m *MemoryTool) save(store map[string]string) error {
	path := m.memoryPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal memory: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (m *MemoryTool) Execute(args map[string]any) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	action, ok := args["action"].(string)
	if !ok || action == "" {
		return "", fmt.Errorf("action is required")
	}

	store, err := m.load()
	if err != nil {
		return "", err
	}

	switch action {
	case "write":
		key, ok := args["key"].(string)
		if !ok || key == "" {
			return "", fmt.Errorf("key is required for write")
		}
		value, _ := args["value"].(string)
		store[key] = value
		if err := m.save(store); err != nil {
			return "", err
		}
		return fmt.Sprintf("saved: %s = %s", key, value), nil

	case "read":
		key, ok := args["key"].(string)
		if !ok || key == "" {
			return "", fmt.Errorf("key is required for read")
		}
		value, exists := store[key]
		if !exists {
			return fmt.Sprintf("key %q not found", key), nil
		}
		return value, nil

	case "list":
		if len(store) == 0 {
			return "memory is empty", nil
		}
		keys := make([]string, 0, len(store))
		for k := range store {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&sb, "%s = %s\n", k, store[k])
		}
		return sb.String(), nil

	case "search":
		term, _ := args["value"].(string)
		if term == "" {
			return "", fmt.Errorf("value (search term) is required for search")
		}
		termLower := strings.ToLower(term)
		var sb strings.Builder
		found := 0
		for k, v := range store {
			if strings.Contains(strings.ToLower(k), termLower) || strings.Contains(strings.ToLower(v), termLower) {
				fmt.Fprintf(&sb, "%s = %s\n", k, v)
				found++
			}
		}
		if found == 0 {
			return fmt.Sprintf("no matches for %q", term), nil
		}
		return sb.String(), nil

	default:
		return "", fmt.Errorf("unknown action: %s (use read, write, list, or search)", action)
	}
}
