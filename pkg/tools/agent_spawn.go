package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

const agentSpawnTimeout = 120 * time.Second

// SubAgentRunner is an interface to break the cycle between tools and agent packages.
// It is implemented by agent.Agent.
type SubAgentRunner interface {
	Run(ctx context.Context, message string) (string, *provider.Usage, error)
}

// SubAgentFactory creates a new sub-agent for spawned tasks. The
// `personaName` parameter is "" for the legacy unnamed-spawn flow
// (sub-agent inherits parent's system prompt) or a named persona
// loaded from ~/.kincode/agents/<name>.md / ~/.localkin/agents/<name>.md.
type SubAgentFactory func(personaName string) SubAgentRunner

// PersonaInfo is what AgentSpawnTool needs to advertise an available
// persona in its tool description without taking a hard dep on
// pkg/agent (cycle prevention). main.go populates this list at boot
// from agent.ListSubAgentPersonas().
type PersonaInfo struct {
	Name        string
	Description string
}

// AgentSpawnTool spawns a sub-agent for parallel tasks.
type AgentSpawnTool struct {
	// Factory creates new sub-agent instances.
	Factory SubAgentFactory
	// Personas advertises named subagents the model can dispatch to
	// by name. Empty list = persona dispatch disabled; the tool
	// behaves like its pre-personas version (single `task` param).
	Personas []PersonaInfo
}

func (a *AgentSpawnTool) Name() string { return "agent_spawn" }

func (a *AgentSpawnTool) Description() string {
	if len(a.Personas) == 0 {
		return "Spawn a sub-agent to run a task in parallel. Returns the sub-agent's response. Timeout: 120s."
	}
	// Build a rich description listing available named subagents.
	// The parent model uses this to decide which persona (if any)
	// to dispatch to. Omitting `agent` preserves the legacy
	// unnamed-spawn flow (subagent inherits parent's system prompt).
	var sb strings.Builder
	sb.WriteString("Spawn a sub-agent to run a task. Use the `agent` param ")
	sb.WriteString("to dispatch to a specialist persona, or omit it for a ")
	sb.WriteString("generic subagent that inherits your system prompt. ")
	sb.WriteString("Available personas:\n")
	for _, p := range a.Personas {
		fmt.Fprintf(&sb, "  • %s — %s\n", p.Name, p.Description)
	}
	sb.WriteString("Timeout: 120s.")
	return sb.String()
}

func (a *AgentSpawnTool) Def() provider.ToolDef {
	props := map[string]any{
		"task": map[string]any{
			"type":        "string",
			"description": "The task description for the sub-agent to execute",
		},
	}
	if len(a.Personas) > 0 {
		// Surface the persona name as an enum so the model gets a
		// validation hint. Empty/missing = generic subagent with
		// parent's system prompt.
		names := make([]string, 0, len(a.Personas))
		for _, p := range a.Personas {
			names = append(names, p.Name)
		}
		props["agent"] = map[string]any{
			"type":        "string",
			"enum":        names,
			"description": "Optional named persona. Omit for a generic subagent.",
		}
	}
	return provider.NewToolDef("agent_spawn", a.Description(), map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []string{"task"},
	})
}

func (a *AgentSpawnTool) Execute(args map[string]any) (string, error) {
	task, ok := args["task"].(string)
	if !ok || task == "" {
		return "", fmt.Errorf("task is required")
	}

	if a.Factory == nil {
		return "", fmt.Errorf("no sub-agent factory configured")
	}

	// Optional: dispatch to a named persona. Empty/missing = legacy
	// generic subagent (inherits parent's system prompt).
	personaName, _ := args["agent"].(string)

	subAgent := a.Factory(personaName)

	ctx, cancel := context.WithTimeout(context.Background(), agentSpawnTimeout)
	defer cancel()

	// Run in a goroutine and collect result.
	type result struct {
		content string
		err     error
	}
	ch := make(chan result, 1)

	go func() {
		content, _, err := subAgent.Run(ctx, task)
		ch <- result{content: content, err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return "", fmt.Errorf("sub-agent error: %w", r.err)
		}
		return r.content, nil
	case <-ctx.Done():
		return "", fmt.Errorf("sub-agent timed out after %v", agentSpawnTimeout)
	}
}
