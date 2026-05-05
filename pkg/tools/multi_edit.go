package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

// MultiEditTool applies a sequence of find-and-replace edits to a
// single file atomically — all edits land or none do. Saves the
// agent ~Nx tokens vs. calling file_edit N times for N changes,
// AND prevents partial-write corruption when an intermediate edit
// fails (e.g. old_string ambiguous after a prior edit changed the
// file).
//
// Sequencing: edits run in array order against the in-memory buffer.
// Each edit's old_string must match against the post-previous-edits
// state, NOT the original file. So edit[1] sees the result of
// edit[0]; edit[2] sees the result of edit[0]+edit[1]; etc. Pattern
// matches Claude Code's MultiEdit semantics so soul prompts written
// for Claude port directly.
//
// Failure mode: if any edit's old_string is missing or ambiguous
// (and replace_all=false), NO write happens. The error message
// names which edit (1-indexed) failed and why, so the agent can
// fix the bad edit + retry without worrying about half-applied state.
type MultiEditTool struct{}

func (m *MultiEditTool) Name() string { return "multi_edit" }

func (m *MultiEditTool) Description() string {
	return "Apply multiple find-and-replace edits to a single file atomically. " +
		"All edits succeed or none do — no partial writes. Each edit's old_string " +
		"matches against the file state AFTER previous edits in the same call. " +
		"Use this instead of multiple file_edit calls when changing multiple " +
		"places in one file: cheaper (1 round vs N), safer (atomic), and the " +
		"agent doesn't have to track intermediate state across rounds."
}

func (m *MultiEditTool) Def() provider.ToolDef {
	return provider.NewToolDef("multi_edit", m.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file",
			},
			"edits": map[string]any{
				"type":        "array",
				"description": "Ordered list of edits to apply. Each edit is {old_string, new_string, replace_all?}.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"old_string": map[string]any{
							"type":        "string",
							"description": "Text to find. Must be unique in file unless replace_all=true.",
						},
						"new_string": map[string]any{
							"type":        "string",
							"description": "Text to replace with. Use empty string to delete.",
						},
						"replace_all": map[string]any{
							"type":        "boolean",
							"description": "If true, replace every occurrence; otherwise old_string must be unique.",
						},
					},
					"required": []string{"old_string", "new_string"},
				},
			},
		},
		"required": []string{"file_path", "edits"},
	})
}

// editSpec is one entry in the edits array. We accept it as
// map[string]any from the tool args (provider returns generic JSON)
// and validate field-by-field.
type editSpec struct {
	OldString  string
	NewString  string
	ReplaceAll bool
}

func (m *MultiEditTool) Execute(args map[string]any) (string, error) {
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	rawEdits, ok := args["edits"].([]any)
	if !ok {
		// Some providers serialize the array via different concrete
		// types (e.g. []map[string]any). Round-trip through JSON to
		// normalize — same trick the file_write parameters dance uses.
		blob, err := json.Marshal(args["edits"])
		if err != nil {
			return "", fmt.Errorf("edits must be an array of {old_string, new_string, replace_all?}")
		}
		var parsed []map[string]any
		if err := json.Unmarshal(blob, &parsed); err != nil {
			return "", fmt.Errorf("edits must be an array of {old_string, new_string, replace_all?}")
		}
		rawEdits = make([]any, len(parsed))
		for i, p := range parsed {
			rawEdits[i] = p
		}
	}
	if len(rawEdits) == 0 {
		return "", fmt.Errorf("edits array is empty")
	}

	specs := make([]editSpec, len(rawEdits))
	for i, raw := range rawEdits {
		m, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("edit %d is not an object", i+1)
		}
		old, _ := m["old_string"].(string)
		if old == "" {
			return "", fmt.Errorf("edit %d: old_string is required and must be non-empty", i+1)
		}
		newStr, _ := m["new_string"].(string)
		ra := false
		if v, ok := m["replace_all"].(bool); ok {
			ra = v
		}
		specs[i] = editSpec{OldString: old, NewString: newStr, ReplaceAll: ra}
	}

	original, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	// Apply edits sequentially against the in-memory buffer. Bail
	// (without writing) on first failure — that's the atomicity
	// guarantee. Track per-edit replacement counts for the report.
	content := string(original)
	counts := make([]int, len(specs))
	for i, e := range specs {
		n := strings.Count(content, e.OldString)
		if n == 0 {
			return "", fmt.Errorf(
				"edit %d failed: old_string not found in %s (after applying %d prior edits). No changes written.",
				i+1, filePath, i)
		}
		if n > 1 && !e.ReplaceAll {
			return "", fmt.Errorf(
				"edit %d failed: old_string occurs %d times in %s after %d prior edits — must be unique or set replace_all=true. No changes written.",
				i+1, n, filePath, i)
		}
		if e.ReplaceAll {
			content = strings.ReplaceAll(content, e.OldString, e.NewString)
			counts[i] = n
		} else {
			content = strings.Replace(content, e.OldString, e.NewString, 1)
			counts[i] = 1
		}
	}

	// All edits validated — single atomic write.
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	// Report: per-edit count + colored diff against the ORIGINAL (so
	// the agent sees the net effect of all edits at once, not edit-
	// by-edit, which would be visually noisy).
	diff := generateDiff(string(original), content, filePath)
	totalChanges := 0
	for _, c := range counts {
		totalChanges += c
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "applied %d edits (%d total replacements) to %s\n",
		len(specs), totalChanges, filePath)
	for i, c := range counts {
		fmt.Fprintf(&sb, "  %d. %d × \"%s\" → \"%s\"\n",
			i+1, c,
			truncate(specs[i].OldString, 40),
			truncate(specs[i].NewString, 40))
	}
	sb.WriteString(diff)
	return sb.String(), nil
}

// truncate shortens a string for display. Used in the per-edit
// summary so a 500-line `old_string` doesn't make the result
// unreadable.
func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
