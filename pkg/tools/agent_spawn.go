package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/LocalKinAI/kincode/pkg/provider"
)

const agentSpawnTimeout = 120 * time.Second

// SubAgentRunner is an interface to break the cycle between tools and agent packages.
// It is implemented by agent.Agent.
type SubAgentRunner interface {
	Run(ctx context.Context, message string) (string, *provider.Usage, error)
}

// SubAgentFactory creates a new sub-agent for spawned tasks.
type SubAgentFactory func() SubAgentRunner

// AgentSpawnTool spawns a sub-agent for parallel tasks.
type AgentSpawnTool struct {
	// Factory creates new sub-agent instances.
	Factory SubAgentFactory
}

func (a *AgentSpawnTool) Name() string { return "agent_spawn" }

func (a *AgentSpawnTool) Description() string {
	return "Spawn a sub-agent to run a task in parallel. Returns the sub-agent's response. Timeout: 120s."
}

func (a *AgentSpawnTool) Def() provider.ToolDef {
	return provider.NewToolDef("agent_spawn", a.Description(), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "The task description for the sub-agent to execute",
			},
		},
		"required": []string{"task"},
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

	subAgent := a.Factory()

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
