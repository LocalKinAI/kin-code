// Package provider defines the LLM provider interface and common types.
package provider

import "context"

// Provider is the interface that all LLM backends must implement.
type Provider interface {
	// Chat sends messages and returns a complete response.
	Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error)
	// Stream sends messages and streams response chunks via onChunk callback.
	Stream(ctx context.Context, messages []Message, tools []ToolDef, onChunk func(string)) (*Response, error)
	// Name returns the provider name (e.g. "anthropic", "openai").
	Name() string
}

// Message represents a single message in the conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Function ToolFunction `json:"function"`
}

// ToolFunction holds the name and arguments for a tool call.
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Response is the provider's complete response.
type Response struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
	StopReason string
}

// Usage tracks token consumption.
type Usage struct {
	Input  int
	Output int
}

// ToolDef defines a tool for the provider API.
type ToolDef struct {
	Type     string         `json:"type"`
	Function ToolDefFunction `json:"function"`
}

// ToolDefFunction is the function definition within a tool.
type ToolDefFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// NewToolDef creates a ToolDef with type "function".
func NewToolDef(name, description string, parameters map[string]any) ToolDef {
	return ToolDef{
		Type: "function",
		Function: ToolDefFunction{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}
