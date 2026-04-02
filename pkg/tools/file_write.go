package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LocalKinAI/kin-code/pkg/provider"
)

// FileWriteTool writes content to a file, creating parent directories as needed.
type FileWriteTool struct{}

func (f *FileWriteTool) Name() string { return "file_write" }

func (f *FileWriteTool) Description() string {
	return "Write content to a file. Creates parent directories if needed. Set overwrite=true to replace existing files."
}

func (f *FileWriteTool) Def() provider.ToolDef {
	return provider.NewToolDef("file_write", f.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
			"overwrite": map[string]any{
				"type":        "boolean",
				"description": "Allow overwriting existing files (default false)",
			},
		},
		"required": []string{"file_path", "content"},
	})
}

func (f *FileWriteTool) Execute(args map[string]any) (string, error) {
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("content is required")
	}

	overwrite := false
	if o, ok := args["overwrite"].(bool); ok {
		overwrite = o
	}

	// Check if file exists.
	if _, err := os.Stat(filePath); err == nil && !overwrite {
		return "", fmt.Errorf("file already exists: %s (set overwrite=true to replace)", filePath)
	}

	// Create parent directories.
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(content), filePath), nil
}
