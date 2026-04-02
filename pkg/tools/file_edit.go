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

	// Generate colored diff output.
	diff := generateDiff(content, newContent, filePath)

	if replaceAll {
		return fmt.Sprintf("replaced %d occurrences in %s\n%s", count, filePath, diff), nil
	}
	return fmt.Sprintf("replaced 1 occurrence in %s\n%s", filePath, diff), nil
}

const (
	diffColorRed   = "\033[31m"
	diffColorGreen = "\033[32m"
	diffColorGray  = "\033[90m"
	diffColorReset = "\033[0m"
)

// generateDiff produces a colored unified-style diff between old and new content.
func generateDiff(oldContent, newContent, filePath string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Find changed regions by simple line comparison.
	type change struct {
		oldStart, oldEnd int
		newStart, newEnd int
	}

	var changes []change
	oi, ni := 0, 0
	for oi < len(oldLines) && ni < len(newLines) {
		if oldLines[oi] == newLines[ni] {
			oi++
			ni++
			continue
		}
		// Found a difference — find extent.
		oStart, nStart := oi, ni
		// Look ahead in new for where old resumes.
		found := false
		for look := 1; look < 50 && (oi+look < len(oldLines) || ni+look < len(newLines)); look++ {
			// Check if old[oi+look] matches new[ni+look].
			if oi+look < len(oldLines) && ni+look < len(newLines) && oldLines[oi+look] == newLines[ni+look] {
				changes = append(changes, change{oStart, oi + look, nStart, ni + look})
				oi += look
				ni += look
				found = true
				break
			}
		}
		if !found {
			changes = append(changes, change{oStart, len(oldLines), nStart, len(newLines)})
			oi = len(oldLines)
			ni = len(newLines)
		}
	}
	// Handle trailing lines.
	if oi < len(oldLines) || ni < len(newLines) {
		changes = append(changes, change{oi, len(oldLines), ni, len(newLines)})
	}

	if len(changes) == 0 {
		return ""
	}

	var sb strings.Builder
	contextSize := 3

	for _, c := range changes {
		// Show context before.
		ctxStart := c.oldStart - contextSize
		if ctxStart < 0 {
			ctxStart = 0
		}
		for i := ctxStart; i < c.oldStart; i++ {
			fmt.Fprintf(&sb, "%s %s%s\n", diffColorGray, oldLines[i], diffColorReset)
		}
		// Show removed lines.
		for i := c.oldStart; i < c.oldEnd; i++ {
			fmt.Fprintf(&sb, "%s-%s%s\n", diffColorRed, oldLines[i], diffColorReset)
		}
		// Show added lines.
		for i := c.newStart; i < c.newEnd; i++ {
			fmt.Fprintf(&sb, "%s+%s%s\n", diffColorGreen, newLines[i], diffColorReset)
		}
		// Show context after.
		ctxEnd := c.oldEnd + contextSize
		if ctxEnd > len(oldLines) {
			ctxEnd = len(oldLines)
		}
		for i := c.oldEnd; i < ctxEnd; i++ {
			fmt.Fprintf(&sb, "%s %s%s\n", diffColorGray, oldLines[i], diffColorReset)
		}
	}

	return sb.String()
}
