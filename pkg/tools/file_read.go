package tools

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

const maxReadLines = 10000

// FileReadTool reads file contents with optional offset and limit.
type FileReadTool struct{}

func (f *FileReadTool) Name() string { return "file_read" }

func (f *FileReadTool) Description() string {
	return "Read a file's contents. Supports offset and limit parameters for large files. Max 10000 lines."
}

func (f *FileReadTool) Def() provider.ToolDef {
	return provider.NewToolDef("file_read", f.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to read",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Line number to start reading from (1-based, default 1)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of lines to read (default 2000, max 10000)",
			},
		},
		"required": []string{"file_path"},
	})
}

func (f *FileReadTool) Execute(args map[string]any) (string, error) {
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	offset := 1
	if o, ok := args["offset"].(float64); ok && o > 0 {
		offset = int(o)
	}

	limit := 2000
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > maxReadLines {
			limit = maxReadLines
		}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var lines []string
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum < offset {
			continue
		}
		if lineNum >= offset+limit {
			break
		}
		lines = append(lines, fmt.Sprintf("%d\t%s", lineNum, scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	if len(lines) == 0 {
		return fmt.Sprintf("(file has %d lines, offset %d returned no content)", lineNum, offset), nil
	}

	return strings.Join(lines, "\n"), nil
}
