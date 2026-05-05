// Package permission handles tool call approval and safety checks.
package permission

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Manager controls whether tool calls require user confirmation.
type Manager struct {
	yolo      bool
	planMode  bool
	reader    *bufio.Reader
	blocklist []string
}

// readOnlyToolsForPlanMode is the allowlist of tool names the agent
// may invoke while plan mode is on. Anything else (bash, file_write,
// file_edit, multi_edit, agent_spawn, todo_write, memory) is denied
// at the permission layer with a planmode-specific error so the
// model knows to stay in research/planning mode and emit a markdown
// plan instead of starting to modify.
var readOnlyToolsForPlanMode = map[string]bool{
	"file_read":  true,
	"glob":       true,
	"grep":       true,
	"web_fetch":  true,
	"web_search": true,
}

// New creates a permission manager.
// If yolo is true, all tool calls are auto-approved (except always-blocked commands).
func New(yolo bool) *Manager {
	return &Manager{
		yolo:   yolo,
		reader: bufio.NewReader(os.Stdin),
		blocklist: []string{
			"rm -rf /",
			"rm -rf /*",
			"sudo rm -rf",
			"mkfs.",
			":(){:|:&};:",
			"> /dev/sda",
			"dd if=/dev/zero of=/dev/sda",
			"chmod -R 777 /",
			"curl | sh",
			"wget | sh",
		},
	}
}

// SetPlanMode toggles plan mode. While enabled, tool calls outside
// the read-only allowlist are denied at CheckPlanMode time — the
// agent loop surfaces the denial as a tool error to the model, which
// learns to switch to "describe what you'd do" instead of doing it.
func (m *Manager) SetPlanMode(enabled bool) { m.planMode = enabled }

// PlanMode returns the current plan-mode state. Used by the server
// to report status on /api/state.
func (m *Manager) PlanMode() bool { return m.planMode }

// CheckPlanMode returns an error if plan mode is on and the named
// tool isn't in the read-only allowlist. The error text is shaped
// for the model — it includes the allowlist so the model can pivot
// to a tool it CAN use rather than guess.
func (m *Manager) CheckPlanMode(toolName string) error {
	if !m.planMode {
		return nil
	}
	if readOnlyToolsForPlanMode[toolName] {
		return nil
	}
	return fmt.Errorf(
		"PLAN MODE: %q is not allowed while planning. "+
			"You may only use read-only tools "+
			"(file_read, glob, grep, web_fetch, web_search). "+
			"Finish your investigation, then end your response with a "+
			"clear markdown plan for the user to approve",
		toolName)
}

// CheckBash checks if a bash command is allowed. Returns an error if blocked.
func (m *Manager) CheckBash(command string) error {
	cmdLower := strings.ToLower(strings.TrimSpace(command))
	for _, blocked := range m.blocklist {
		if strings.Contains(cmdLower, strings.ToLower(blocked)) {
			return fmt.Errorf("BLOCKED: command matches dangerous pattern %q", blocked)
		}
	}
	return nil
}

// Confirm asks the user for permission to execute a tool call.
// Returns true if approved. In yolo mode, always returns true.
func (m *Manager) Confirm(toolName string, summary string) bool {
	if m.yolo {
		return true
	}

	// Read-only tools don't need confirmation.
	switch toolName {
	case "file_read", "glob", "grep":
		return true
	}

	fmt.Printf("\n\033[33m⚡ %s\033[0m\n", toolName)
	if summary != "" {
		fmt.Printf("   %s\n", summary)
	}
	fmt.Print("\033[33mAllow? [Y/n/q] \033[0m")

	line, err := m.reader.ReadString('\n')
	if err != nil {
		return false
	}

	line = strings.TrimSpace(strings.ToLower(line))
	switch line {
	case "", "y", "yes":
		return true
	case "q", "quit":
		fmt.Println("Aborting.")
		os.Exit(0)
		return false
	default:
		return false
	}
}
