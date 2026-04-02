// Package agent implements the core agent loop: message -> provider -> tools -> loop.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/LocalKinAI/kin-code/pkg/permission"
	"github.com/LocalKinAI/kin-code/pkg/provider"
	"github.com/LocalKinAI/kin-code/pkg/tools"
)

const defaultMaxRounds = 25

// Agent orchestrates the conversation between user, provider, and tools.
type Agent struct {
	provider     provider.Provider
	tools        *tools.Registry
	permissions  *permission.Manager
	messages     []provider.Message
	systemPrompt string
	maxRounds    int
}

// Config holds agent configuration.
type Config struct {
	Provider     provider.Provider
	Tools        *tools.Registry
	Permissions  *permission.Manager
	SystemPrompt string
	MaxRounds    int
}

// New creates a new Agent.
func New(cfg Config) *Agent {
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultMaxRounds
	}

	a := &Agent{
		provider:     cfg.Provider,
		tools:        cfg.Tools,
		permissions:  cfg.Permissions,
		systemPrompt: cfg.SystemPrompt,
		maxRounds:    maxRounds,
	}

	// Add system prompt as first message.
	if cfg.SystemPrompt != "" {
		a.messages = append(a.messages, provider.Message{
			Role:    "system",
			Content: cfg.SystemPrompt,
		})
	}

	return a
}

// Run processes a user message through the agent loop.
// It streams output to the terminal and returns the final text response.
func (a *Agent) Run(ctx context.Context, userMessage string) (string, *provider.Usage, error) {
	a.messages = append(a.messages, provider.Message{
		Role:    "user",
		Content: userMessage,
	})

	totalUsage := &provider.Usage{}

	for round := 0; round < a.maxRounds; round++ {
		toolDefs := a.tools.Defs()

		resp, err := a.provider.Stream(ctx, a.messages, toolDefs, func(chunk string) {
			fmt.Print(chunk)
		})
		if err != nil {
			return "", totalUsage, fmt.Errorf("provider error: %w", err)
		}

		totalUsage.Input += resp.Usage.Input
		totalUsage.Output += resp.Usage.Output

		// Add assistant response to history.
		assistantMsg := provider.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		a.messages = append(a.messages, assistantMsg)

		// If no tool calls, we're done.
		if len(resp.ToolCalls) == 0 {
			if resp.Content != "" {
				fmt.Println() // newline after streaming
			}
			return resp.Content, totalUsage, nil
		}

		fmt.Println() // newline after any streamed content

		// Execute tool calls.
		for _, tc := range resp.ToolCalls {
			result, err := a.executeTool(tc)
			if err != nil {
				result = fmt.Sprintf("Error: %s", err)
			}

			// Add tool result to messages.
			a.messages = append(a.messages, provider.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", totalUsage, fmt.Errorf("reached max rounds (%d) without completing", a.maxRounds)
}

func (a *Agent) executeTool(tc provider.ToolCall) (string, error) {
	tool, err := a.tools.Get(tc.Function.Name)
	if err != nil {
		return "", err
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("parse tool arguments: %w", err)
	}

	// Permission check for bash commands.
	if tc.Function.Name == "bash" {
		if cmd, ok := args["command"].(string); ok {
			if err := a.permissions.CheckBash(cmd); err != nil {
				return "", err
			}
		}
	}

	// Get a summary for confirmation prompt.
	summary := toolSummary(tc.Function.Name, args)

	if !a.permissions.Confirm(tc.Function.Name, summary) {
		return "Tool call denied by user.", nil
	}

	// Show tool execution.
	fmt.Fprintf(os.Stderr, "\033[2m[%s] %s\033[0m\n", tc.Function.Name, summary)

	result, err := tool.Execute(args)
	if err != nil {
		return fmt.Sprintf("Tool error: %s\nOutput:\n%s", err, result), nil
	}

	return result, nil
}

func toolSummary(name string, args map[string]any) string {
	switch name {
	case "bash":
		if cmd, ok := args["command"].(string); ok {
			if len(cmd) > 80 {
				return cmd[:80] + "..."
			}
			return cmd
		}
	case "file_read":
		if fp, ok := args["file_path"].(string); ok {
			return fp
		}
	case "file_write":
		if fp, ok := args["file_path"].(string); ok {
			return fp
		}
	case "file_edit":
		if fp, ok := args["file_path"].(string); ok {
			return fp
		}
	case "glob":
		if p, ok := args["pattern"].(string); ok {
			return p
		}
	case "grep":
		if p, ok := args["pattern"].(string); ok {
			parts := []string{p}
			if path, ok := args["path"].(string); ok {
				parts = append(parts, "in "+path)
			}
			return strings.Join(parts, " ")
		}
	}
	return ""
}

// Clear resets the conversation history, keeping the system prompt.
func (a *Agent) Clear() {
	var msgs []provider.Message
	if a.systemPrompt != "" {
		msgs = append(msgs, provider.Message{
			Role:    "system",
			Content: a.systemPrompt,
		})
	}
	a.messages = msgs
}

// Compact summarizes the conversation to reduce context size.
func (a *Agent) Compact(ctx context.Context) error {
	if len(a.messages) < 4 {
		return nil
	}

	// Keep system prompt and ask provider to summarize.
	summaryReq := []provider.Message{
		{Role: "system", Content: "Summarize the following conversation concisely, preserving key decisions, file paths, and code changes. Be brief."},
		{Role: "user", Content: fmt.Sprintf("Summarize this conversation:\n\n%s", formatMessages(a.messages))},
	}

	resp, err := a.provider.Chat(ctx, summaryReq, nil)
	if err != nil {
		return fmt.Errorf("compact failed: %w", err)
	}

	// Reset with system prompt + summary.
	a.messages = nil
	if a.systemPrompt != "" {
		a.messages = append(a.messages, provider.Message{
			Role:    "system",
			Content: a.systemPrompt,
		})
	}
	a.messages = append(a.messages, provider.Message{
		Role:    "user",
		Content: "[Previous conversation summary]\n" + resp.Content,
	})
	a.messages = append(a.messages, provider.Message{
		Role:    "assistant",
		Content: "Understood. I have the context from our previous conversation. How can I help?",
	})

	return nil
}

func formatMessages(msgs []provider.Message) string {
	var parts []string
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]: %s", m.Role, m.Content))
	}
	return strings.Join(parts, "\n\n")
}
