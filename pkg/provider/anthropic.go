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
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropic creates a new Anthropic provider.
func NewAnthropic(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

func (a *AnthropicProvider) Name() string { return "anthropic" }

// anthropicRequest is the request body for the Messages API.
type anthropicRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []anthropicMsg   `json:"messages"`
	Tools     []anthropicTool  `json:"tools,omitempty"`
	Stream    bool             `json:"stream,omitempty"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
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

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicAPI, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

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
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				currentToolCall = &ToolCall{
					ID: event.ContentBlock.ID,
					Function: ToolFunction{
						Name: event.ContentBlock.Name,
					},
				}
				currentToolInput.Reset()
			}
		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
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
