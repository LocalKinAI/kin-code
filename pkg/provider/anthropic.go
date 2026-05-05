package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	anthropicAPI     = "https://api.anthropic.com/v1/messages"
	anthropicVersion = "2023-06-01"
)

// AnthropicProvider implements Provider for the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey         string
	model          string
	isOAuth        bool // true = use Bearer auth instead of x-api-key
	client         *http.Client
	thinking       bool
	thinkingBudget int
	onThinking     func(string) // callback for thinking content display
}

// NewAnthropic creates a new Anthropic provider.
func NewAnthropic(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

// SetOAuth marks this provider as using OAuth Bearer auth instead of x-api-key.
func (a *AnthropicProvider) SetOAuth(isOAuth bool) {
	a.isOAuth = isOAuth
}

// SetThinking enables or disables extended thinking with the given token budget.
func (a *AnthropicProvider) SetThinking(enabled bool, budget int) {
	a.thinking = enabled
	a.thinkingBudget = budget
	if a.thinkingBudget <= 0 {
		a.thinkingBudget = 10000
	}
}

// SetOnThinking sets a callback for displaying thinking content.
func (a *AnthropicProvider) SetOnThinking(fn func(string)) {
	a.onThinking = fn
}

func (a *AnthropicProvider) Name() string { return "anthropic" }

// anthropicRequest is the request body for the Messages API.
type anthropicRequest struct {
	Model     string                  `json:"model"`
	MaxTokens int                     `json:"max_tokens"`
	System    string                  `json:"system,omitempty"`
	Messages  []anthropicMsg          `json:"messages"`
	Tools     []anthropicTool         `json:"tools,omitempty"`
	Stream    bool                    `json:"stream,omitempty"`
	Thinking  *anthropicThinkingConfig `json:"thinking,omitempty"`
}

// anthropicThinkingConfig controls extended thinking.
type anthropicThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     any                   `json:"input,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	Content   string                `json:"content,omitempty"`
	Source    *anthropicImageSource `json:"source,omitempty"`
}

// anthropicImageSource matches Anthropic's image content block shape:
//
//	{"type": "image", "source": {"type": "base64",
//	  "media_type": "image/png", "data": "<base64>"}}
//
// We only use base64 sources — URL sources require the model to
// fetch the image which adds an extra round-trip and fails for
// localhost / private images. Base64 keeps the payload self-contained.
type anthropicImageSource struct {
	Type      string `json:"type"`       // always "base64"
	MediaType string `json:"media_type"` // image/png, image/jpeg, etc
	Data      string `json:"data"`       // raw base64
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Chat sends a non-streaming request to Anthropic.
func (a *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error) {
	return a.doRequest(ctx, messages, tools, false, nil)
}

// Stream sends a streaming request to Anthropic.
func (a *AnthropicProvider) Stream(ctx context.Context, messages []Message, tools []ToolDef, onChunk func(string)) (*Response, error) {
	return a.doRequest(ctx, messages, tools, true, onChunk)
}

func (a *AnthropicProvider) doRequest(ctx context.Context, messages []Message, tools []ToolDef, stream bool, onChunk func(string)) (*Response, error) {
	// Convert messages to Anthropic format.
	var system string
	var aMsgs []anthropicMsg

	for _, m := range messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}

		if m.Role == "tool" {
			// Tool result messages become user messages with tool_result content blocks.
			aMsgs = append(aMsgs, anthropicMsg{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: m.ToolCallID,
					Content:   m.Content,
				}},
			})
			continue
		}

		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// Assistant message with tool calls.
			var blocks []anthropicContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			aMsgs = append(aMsgs, anthropicMsg{Role: "assistant", Content: blocks})
			continue
		}

		// Multimodal user message: translate Blocks into Anthropic's
		// content array. Text blocks → {type:text, text:...}, image
		// blocks → {type:image, source:{base64, media_type, data}}.
		// If Content is also set we prepend it as a text block so
		// callers can pass "user typed this caption + dropped these
		// images" without having to manually wrap the caption.
		if len(m.Blocks) > 0 {
			var blocks []anthropicContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, b := range m.Blocks {
				switch b.Type {
				case "text":
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: b.Text})
				case "image":
					blocks = append(blocks, anthropicContentBlock{
						Type: "image",
						Source: &anthropicImageSource{
							Type:      "base64",
							MediaType: b.ImageMediaType,
							Data:      b.ImageBase64,
						},
					})
				}
				// Unknown block types are silently dropped — keeps
				// the path forgiving when one provider gains a block
				// kind before the other.
			}
			aMsgs = append(aMsgs, anthropicMsg{Role: m.Role, Content: blocks})
			continue
		}

		aMsgs = append(aMsgs, anthropicMsg{Role: m.Role, Content: m.Content})
	}

	// Convert tools.
	var aTools []anthropicTool
	for _, t := range tools {
		aTools = append(aTools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	reqBody := anthropicRequest{
		Model:     a.model,
		MaxTokens: 8192,
		System:    system,
		Messages:  aMsgs,
		Tools:     aTools,
		Stream:    stream,
	}

	// Add extended thinking if enabled.
	if a.thinking {
		reqBody.Thinking = &anthropicThinkingConfig{
			Type:         "enabled",
			BudgetTokens: a.thinkingBudget,
		}
		// Extended thinking requires higher max_tokens.
		if reqBody.MaxTokens < a.thinkingBudget+4096 {
			reqBody.MaxTokens = a.thinkingBudget + 4096
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicAPI, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", anthropicVersion)
	if a.isOAuth {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		req.Header.Set("x-api-key", a.apiKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, string(b))
	}

	if stream {
		return a.handleStream(resp.Body, onChunk)
	}

	var aResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&aResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return a.convertResponse(&aResp), nil
}

func (a *AnthropicProvider) handleStream(body io.Reader, onChunk func(string)) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	result := &Response{}
	var toolCalls []ToolCall
	var currentToolCall *ToolCall
	var currentToolInput strings.Builder
	var contentBuilder strings.Builder
	var inThinkingBlock bool

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Message *struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil {
				result.Usage.Input = event.Message.Usage.InputTokens
			}
		case "content_block_start":
			if event.ContentBlock != nil {
				switch event.ContentBlock.Type {
				case "tool_use":
					currentToolCall = &ToolCall{
						ID: event.ContentBlock.ID,
						Function: ToolFunction{
							Name: event.ContentBlock.Name,
						},
					}
					currentToolInput.Reset()
				case "thinking":
					inThinkingBlock = true
					if a.onThinking != nil {
						a.onThinking("\033[2m[thinking] ")
					}
				}
			}
		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
				case "thinking_delta":
					if a.onThinking != nil && event.Delta.Text != "" {
						a.onThinking(event.Delta.Text)
					}
				case "text_delta":
					contentBuilder.WriteString(event.Delta.Text)
					if onChunk != nil {
						onChunk(event.Delta.Text)
					}
				case "input_json_delta":
					currentToolInput.WriteString(event.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			if inThinkingBlock {
				inThinkingBlock = false
				if a.onThinking != nil {
					a.onThinking("\033[0m\n")
				}
			}
			if currentToolCall != nil {
				currentToolCall.Function.Arguments = currentToolInput.String()
				toolCalls = append(toolCalls, *currentToolCall)
				currentToolCall = nil
			}
		case "message_delta":
			if event.Delta != nil {
				result.StopReason = event.Delta.StopReason
			}
			if event.Usage != nil {
				result.Usage.Output = event.Usage.OutputTokens
			}
		}
	}

	result.Content = contentBuilder.String()
	result.ToolCalls = toolCalls
	return result, scanner.Err()
}

func (a *AnthropicProvider) convertResponse(resp *anthropicResponse) *Response {
	result := &Response{
		StopReason: resp.StopReason,
		Usage: Usage{
			Input:  resp.Usage.InputTokens,
			Output: resp.Usage.OutputTokens,
		},
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "thinking":
			// Display thinking content in dim text.
			if a.onThinking != nil && block.Text != "" {
				a.onThinking(block.Text)
			}
		case "text":
			result.Content += block.Text
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID: block.ID,
				Function: ToolFunction{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		}
	}

	return result
}
