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
//
// Two content paths coexist:
//
//  1. Text-only: set Content. This is the default and what every
//     existing call site does. Works on every provider unchanged.
//  2. Multimodal: set Blocks (and leave Content empty). Each block
//     is a typed piece of content — text or image — that providers
//     translate to their native format (Anthropic's `content` array,
//     OpenAI's `content` parts list).
//
// When both are set, Blocks wins — providers prepend Content as a
// text block if it's also non-empty, but the canonical multimodal
// path is "Blocks only".
type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Blocks     []ContentBlock `json:"blocks,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// ContentBlock is one piece of a multimodal message: text or image.
//
// Image blocks carry raw base64 + media type — this matches both
// Anthropic's `image.source = {type:base64, media_type, data}` shape
// and OpenAI's `image_url = data:<media>;base64,<data>` data-URL
// shape, so providers translate without re-encoding.
//
// Type ∈ {"text", "image"}. Unknown types are skipped at provider
// translation time rather than erroring — keeps the path forgiving
// for future block kinds (audio, document) added on one provider
// before the other.
type ContentBlock struct {
	Type           string `json:"type"`
	Text           string `json:"text,omitempty"`
	ImageBase64    string `json:"image_base64,omitempty"`
	ImageMediaType string `json:"image_media_type,omitempty"`
}

// TextBlock is a convenience constructor for a text content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// ImageBlock is a convenience constructor for an image content
// block. mediaType should be "image/png" / "image/jpeg" / "image/gif"
// / "image/webp" — the four formats both Anthropic and OpenAI vision
// accept. data is the raw base64-encoded image bytes (no data: URL
// prefix; the provider adds that on the OpenAI path).
func ImageBlock(mediaType, base64Data string) ContentBlock {
	return ContentBlock{
		Type:           "image",
		ImageMediaType: mediaType,
		ImageBase64:    base64Data,
	}
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
