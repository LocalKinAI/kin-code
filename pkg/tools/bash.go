package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/LocalKinAI/kin-code/pkg/provider"
)

const (
	bashTimeout   = 30 * time.Second
	maxOutputSize = 128 * 1024 // 128KB
)

// BashTool executes shell commands.
type BashTool struct{}

func (b *BashTool) Name() string { return "bash" }

func (b *BashTool) Description() string {
	return "Execute a bash command and return its output. Timeout: 30s. Output capped at 128KB."
}

func (b *BashTool) Def() provider.ToolDef {
	return provider.NewToolDef("bash", b.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The bash command to execute",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (default 30, max 300)",
			},
		},
		"required": []string{"command"},
	})
}

// blockedCommands are patterns that should never be executed.
var blockedCommands = []string{
	"rm -rf /",
	"rm -rf /*",
	"sudo rm -rf",
	"mkfs.",
	":(){:|:&};:",
	"> /dev/sda",
	"dd if=/dev/zero of=/dev/sda",
	"chmod -R 777 /",
}

func (b *BashTool) Execute(args map[string]any) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Check blocklist.
	cmdLower := strings.ToLower(strings.TrimSpace(command))
	for _, blocked := range blockedCommands {
		if strings.Contains(cmdLower, strings.ToLower(blocked)) {
			return "", fmt.Errorf("blocked: command matches dangerous pattern %q", blocked)
		}
	}

	timeout := bashTimeout
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
		if timeout > 300*time.Second {
			timeout = 300 * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "STDERR:\n" + stderr.String()
	}

	// Cap output size.
	if len(output) > maxOutputSize {
		output = output[:maxOutputSize] + "\n... (output truncated at 128KB)"
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("command timed out after %v", timeout)
		}
		return output, fmt.Errorf("exit code %d", cmd.ProcessState.ExitCode())
	}

	return output, nil
}
