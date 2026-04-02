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

const defaultOpenAIEndpoint = "https://api.openai.com/v1/chat/completions"

// OpenAIProvider implements Provider for OpenAI-compatible APIs.
// Works with OpenAI, Ollama, DeepSeek, and other compatible endpoints.
type OpenAIProvider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
	name     string
}

// NewOpenAI creates a new OpenAI-compatible provider.
func NewOpenAI(apiKey, model, endpoint string) *OpenAIProvider {
	if endpoint == "" {
		endpoint = defaultOpenAIEndpoint
	}
	name := "openai"
	if strings.Contains(endpoint, "localhost") || strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "11434") {
		name = "ollama"
	}
	return &OpenAIProvider{
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
		client:   &http.Client{},
		name:     name,
	}
}

func (o *OpenAIProvider) Name() string { return o.name }

// OpenAI request/response types.
type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Tools    []ToolDef       `json:"tools,omitempty"`
	Stream   bool            `json:"stream,omitempty"`
}

type openaiMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Chat sends a non-streaming request.
func (o *OpenAIProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error) {
	return o.doRequest(ctx, messages, tools, false, nil)
}

// Stream sends a streaming request.
func (o *OpenAIProvider) Stream(ctx context.Context, messages []Message, tools []ToolDef, onChunk func(string)) (*Response, error) {
	return o.doRequest(ctx, messages, tools, true, onChunk)
}

func (o *OpenAIProvider) doRequest(ctx context.Context, messages []Message, tools []ToolDef, stream bool, onChunk func(string)) (*Response, error) {
	// Convert messages to OpenAI format.
	var oMsgs []openaiMessage
	for _, m := range messages {
		om := openaiMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			om.ToolCalls = m.ToolCalls
		}
		oMsgs = append(oMsgs, om)
	}

	reqBody := openaiRequest{
		Model:    o.model,
		Messages: oMsgs,
		Stream:   stream,
	}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	if stream {
		return o.handleStream(resp.Body, onChunk)
	}

	var oResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return o.convertResponse(&oResp), nil
}

func (o *OpenAIProvider) handleStream(body io.Reader, onChunk func(string)) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	result := &Response{}
	toolCallMap := make(map[int]*ToolCall)
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

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			result.Usage.Input = chunk.Usage.PromptTokens
			result.Usage.Output = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != nil {
			result.StopReason = *choice.FinishReason
		}

		if choice.Delta.Content != "" {
			contentBuilder.WriteString(choice.Delta.Content)
			if onChunk != nil {
				onChunk(choice.Delta.Content)
			}
		}

		for _, tc := range choice.Delta.ToolCalls {
			existing, ok := toolCallMap[tc.Index]
			if !ok {
				existing = &ToolCall{
					ID: tc.ID,
					Function: ToolFunction{
						Name: tc.Function.Name,
					},
				}
				toolCallMap[tc.Index] = existing
			}
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			existing.Function.Arguments += tc.Function.Arguments
		}
	}

	result.Content = contentBuilder.String()
	for i := 0; i < len(toolCallMap); i++ {
		if tc, ok := toolCallMap[i]; ok {
			result.ToolCalls = append(result.ToolCalls, *tc)
		}
	}

	return result, scanner.Err()
}

func (o *OpenAIProvider) convertResponse(resp *openaiResponse) *Response {
	result := &Response{
		Usage: Usage{
			Input:  resp.Usage.PromptTokens,
			Output: resp.Usage.CompletionTokens,
		},
	}

	if len(resp.Choices) > 0 {
		result.Content = resp.Choices[0].Message.Content
		result.ToolCalls = resp.Choices[0].Message.ToolCalls
		result.StopReason = resp.Choices[0].FinishReason
	}

	return result
}
