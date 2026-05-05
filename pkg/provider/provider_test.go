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

func TestMultimodalMessageBlocks(t *testing.T) {
	// A user turn with a caption + one image. Shape we expect callers
	// (server/agent) to construct.
	msg := Message{
		Role:    "user",
		Content: "What's in this picture?",
		Blocks: []ContentBlock{
			ImageBlock("image/png", "iVBORw0KGgoAAAA..."),
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

	if got.Content != msg.Content {
		t.Errorf("Content: expected %q, got %q", msg.Content, got.Content)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(got.Blocks))
	}
	b := got.Blocks[0]
	if b.Type != "image" {
		t.Errorf("Block.Type: expected %q, got %q", "image", b.Type)
	}
	if b.ImageMediaType != "image/png" {
		t.Errorf("ImageMediaType: expected %q, got %q", "image/png", b.ImageMediaType)
	}
	if b.ImageBase64 != "iVBORw0KGgoAAAA..." {
		t.Errorf("ImageBase64 round-trip mismatch")
	}
}

func TestTextBlockHelper(t *testing.T) {
	b := TextBlock("hi")
	if b.Type != "text" || b.Text != "hi" {
		t.Errorf("TextBlock(\"hi\") = %+v, want type=text text=hi", b)
	}
	// Text block must not carry image fields — would confuse providers.
	if b.ImageBase64 != "" || b.ImageMediaType != "" {
		t.Errorf("TextBlock leaked image fields: %+v", b)
	}
}

func TestImageBlockHelper(t *testing.T) {
	b := ImageBlock("image/jpeg", "abc=")
	if b.Type != "image" {
		t.Errorf("Type: expected image, got %q", b.Type)
	}
	if b.ImageMediaType != "image/jpeg" || b.ImageBase64 != "abc=" {
		t.Errorf("ImageBlock fields wrong: %+v", b)
	}
	// And no leaked Text.
	if b.Text != "" {
		t.Errorf("ImageBlock leaked Text: %q", b.Text)
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
