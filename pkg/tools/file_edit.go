package tools

import (
	"fmt"
	"os"
	"strings"

	"github.com/LocalKinAI/kin-code/pkg/provider"
)

// FileEditTool performs find-and-replace edits in files.
type FileEditTool struct{}

func (f *FileEditTool) Name() string { return "file_edit" }

func (f *FileEditTool) Description() string {
	return "Find and replace text in a file. old_string must be unique in the file. Fails if not found or ambiguous."
}

func (f *FileEditTool) Def() provider.ToolDef {
	return provider.NewToolDef("file_edit", f.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to edit",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact text to find and replace",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The replacement text",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences (default false, requires unique match)",
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	})
}

func (f *FileEditTool) Execute(args map[string]any) (string, error) {
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	oldString, ok := args["old_string"].(string)
	if !ok || oldString == "" {
		return "", fmt.Errorf("old_string is required")
	}

	newString, _ := args["new_string"].(string)

	replaceAll := false
	if ra, ok := args["replace_all"].(bool); ok {
		replaceAll = ra
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	content := string(data)
	count := strings.Count(content, oldString)

	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", filePath)
	}

	if count > 1 && !replaceAll {
		return "", fmt.Errorf("old_string found %d times in %s (must be unique, or set replace_all=true)", count, filePath)
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(content, oldString, newString)
	} else {
		newContent = strings.Replace(content, oldString, newString, 1)
	}

	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	if replaceAll {
		return fmt.Sprintf("replaced %d occurrences in %s", count, filePath), nil
	}
	return fmt.Sprintf("replaced 1 occurrence in %s", filePath), nil
}
