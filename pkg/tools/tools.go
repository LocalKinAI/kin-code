// Package tools defines the tool interface and registry for kin-code.
package tools

import (
	"fmt"
	"sync"

	"github.com/LocalKinAI/kin-code/pkg/provider"
)

// Tool is the interface that all tools must implement.
type Tool interface {
	// Name returns the tool's unique identifier.
	Name() string
	// Description returns a human-readable description.
	Description() string
	// Def returns the tool definition for the provider API.
	Def() provider.ToolDef
	// Execute runs the tool with the given arguments.
	Execute(args map[string]any) (string, error)
}

// Registry holds all registered tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return t, nil
}

// All returns all registered tools.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// Defs returns tool definitions for all registered tools.
func (r *Registry) Defs() []provider.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]provider.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t.Def())
	}
	return result
}

// RegisterDefaults registers all built-in tools.
func RegisterDefaults(r *Registry) {
	r.Register(&BashTool{})
	r.Register(&FileReadTool{})
	r.Register(&FileWriteTool{})
	r.Register(&FileEditTool{})
	r.Register(&GlobTool{})
	r.Register(&GrepTool{})
	r.Register(&WebFetchTool{})
	r.Register(&WebSearchTool{})
	r.Register(&MemoryTool{})
}

// RegisterDefaultsWithAgent registers all built-in tools including agent_spawn,
// which requires a factory to create sub-agent instances.
func RegisterDefaultsWithAgent(r *Registry, factory SubAgentFactory) {
	RegisterDefaults(r)
	r.Register(&AgentSpawnTool{
		Factory: factory,
	})
}
