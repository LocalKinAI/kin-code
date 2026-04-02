package provider

import (
	"encoding/json"
	"testing"
)

func TestMessageJSON(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "hello world",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if got.Role != msg.Role {
		t.Errorf("Role: expected %q, got %q", msg.Role, got.Role)
	}
	if got.Content != msg.Content {
		t.Errorf("Content: expected %q, got %q", msg.Content, got.Content)
	}
}

func TestMessageWithToolCallsJSON(t *testing.T) {
	msg := Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCall{
			{
				ID: "call_123",
				Function: ToolFunction{
					Name:      "bash",
					Arguments: `{"command":"echo hi"}`,
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("expected tool call name %q, got %q", "bash", got.ToolCalls[0].Function.Name)
	}
}

func TestToolDefJSON(t *testing.T) {
	td := NewToolDef("test_tool", "A test tool", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{
				"type": "string",
			},
		},
	})

	data, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify the JSON contains expected fields.
	var raw map[string]any
	json.Unmarshal(data, &raw)

	if raw["type"] != "function" {
		t.Errorf("expected type %q, got %v", "function", raw["type"])
	}

	fn, ok := raw["function"].(map[string]any)
	if !ok {
		t.Fatal("expected 'function' key in JSON")
	}
	if fn["name"] != "test_tool" {
		t.Errorf("expected name %q, got %v", "test_tool", fn["name"])
	}
	if fn["description"] != "A test tool" {
		t.Errorf("expected description %q, got %v", "A test tool", fn["description"])
	}
}
