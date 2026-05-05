package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

// TodoWriteTool lets the agent maintain a structured task list
// across rounds within one chat session. The list is overwritten
// in full on every call (caller passes the complete current list,
// not a delta) so semantics are simple: "this is the todo state
// now". Mirrors Claude Code's TodoWrite tool exactly so soul prompts
// written for Claude port over.
//
// State lives in-process on a per-agent basis. The desktop shell
// (KinClaw Mac Code mode) can render this as a checklist UI by
// observing the tool_call events on the SSE stream — the args
// payload IS the list. No additional endpoint needed; the agent
// announces its plan, the UI listens.
//
// Status enum: pending / in_progress / completed. Spec says only
// ONE in_progress at a time (single-tasking discipline). The tool
// doesn't enforce — it's a soul-prompt-level rule — but agents
// trained on Claude Code's docs already know the convention.
type TodoWriteTool struct {
	mu    sync.Mutex
	items []TodoItem
}

// TodoItem matches Claude Code's shape so prompts that say "write
// a todo with content / activeForm / status" produce the right JSON.
//
//	content    — imperative form: "Write tests"
//	activeForm — present-continuous: "Writing tests"
//	status     — pending | in_progress | completed
type TodoItem struct {
	Content    string `json:"content"`
	ActiveForm string `json:"activeForm"`
	Status     string `json:"status"`
}

func (t *TodoWriteTool) Name() string { return "todo_write" }

func (t *TodoWriteTool) Description() string {
	return "Maintain a structured task list across rounds. Pass the complete " +
		"current list each time (not a delta) — the existing list is replaced. " +
		"Use for multi-step work: break the task into 3+ items, mark one as " +
		"in_progress while you do it, mark completed when done, then move to " +
		"the next. Skip for trivial single-step requests."
}

func (t *TodoWriteTool) Def() provider.ToolDef {
	return provider.NewToolDef("todo_write", t.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "The complete current todo list (overwrites prior state)",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "Imperative description, e.g. \"Write tests\"",
						},
						"activeForm": map[string]any{
							"type":        "string",
							"description": "Present-continuous form, e.g. \"Writing tests\"",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed"},
							"description": "pending / in_progress / completed",
						},
					},
					"required": []string{"content", "activeForm", "status"},
				},
			},
		},
		"required": []string{"todos"},
	})
}

func (t *TodoWriteTool) Execute(args map[string]any) (string, error) {
	rawTodos := args["todos"]
	// Round-trip via JSON to handle the various concrete shapes
	// providers serialize arrays as ([]any vs []map[string]any).
	blob, err := json.Marshal(rawTodos)
	if err != nil {
		return "", fmt.Errorf("todos must be an array")
	}
	var parsed []TodoItem
	if err := json.Unmarshal(blob, &parsed); err != nil {
		return "", fmt.Errorf("todos must be an array of {content, activeForm, status}")
	}

	// Validate. Status must be one of the enum values; content +
	// activeForm must be non-empty. Reject the whole batch on bad
	// input rather than silently keeping stale state mixed with new.
	inProgressCount := 0
	for i, item := range parsed {
		if item.Content == "" {
			return "", fmt.Errorf("todo %d: content is required", i+1)
		}
		if item.ActiveForm == "" {
			return "", fmt.Errorf("todo %d: activeForm is required", i+1)
		}
		switch item.Status {
		case "pending", "in_progress", "completed":
		default:
			return "", fmt.Errorf("todo %d: status must be pending|in_progress|completed (got %q)",
				i+1, item.Status)
		}
		if item.Status == "in_progress" {
			inProgressCount++
		}
	}

	t.mu.Lock()
	t.items = parsed
	t.mu.Unlock()

	// Render a compact human-readable view for the assistant's
	// follow-up reasoning — the agent often reads its own previous
	// tool result to plan the next step.
	var sb strings.Builder
	fmt.Fprintf(&sb, "todo list updated (%d items, %d in_progress, %d completed):\n",
		len(parsed), inProgressCount, countStatus(parsed, "completed"))
	for i, item := range parsed {
		mark := "[ ]"
		switch item.Status {
		case "in_progress":
			mark = "[~]"
		case "completed":
			mark = "[x]"
		}
		label := item.Content
		if item.Status == "in_progress" {
			label = item.ActiveForm
		}
		fmt.Fprintf(&sb, "  %d. %s %s\n", i+1, mark, label)
	}
	if inProgressCount > 1 {
		sb.WriteString("\nWarning: more than one item is in_progress. Convention is single-tasking — keep one active.\n")
	}
	return sb.String(), nil
}

func countStatus(items []TodoItem, status string) int {
	n := 0
	for _, it := range items {
		if it.Status == status {
			n++
		}
	}
	return n
}

// Items returns a snapshot of the current todo list. Used by the
// HTTP server (future endpoint) or by tests to inspect state without
// going through Execute.
func (t *TodoWriteTool) Items() []TodoItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TodoItem, len(t.items))
	copy(out, t.items)
	return out
}
