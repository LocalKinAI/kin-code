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
	reader    *bufio.Reader
	blocklist []string
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
