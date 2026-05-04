package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LocalKinAI/kincode/pkg/permission"
	"github.com/LocalKinAI/kincode/pkg/provider"
	"github.com/LocalKinAI/kincode/pkg/tools"
)

// mockProvider implements provider.Provider for testing without network calls.
type mockProvider struct{}

func (m *mockProvider) Chat(_ context.Context, msgs []provider.Message, _ []provider.ToolDef) (*provider.Response, error) {
	return &provider.Response{
		Content: "mock response",
		Usage:   provider.Usage{Input: 10, Output: 5},
	}, nil
}

func (m *mockProvider) Stream(_ context.Context, msgs []provider.Message, _ []provider.ToolDef, onChunk func(string)) (*provider.Response, error) {
	return &provider.Response{
		Content: "mock response",
		Usage:   provider.Usage{Input: 10, Output: 5},
	}, nil
}

func (m *mockProvider) Name() string { return "mock" }

func TestAgentNew(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterDefaults(reg)
	perm := permission.New(true)

	a := New(Config{
		Provider:     &mockProvider{},
		Tools:        reg,
		Permissions:  perm,
		SystemPrompt: "You are a helpful assistant.",
	})

	if a.SystemPrompt() != "You are a helpful assistant." {
		t.Errorf("expected system prompt %q, got %q", "You are a helpful assistant.", a.SystemPrompt())
	}
}

func TestAgentMessages(t *testing.T) {
	reg := tools.NewRegistry()
	perm := permission.New(true)

	a := New(Config{
		Provider:     &mockProvider{},
		Tools:        reg,
		Permissions:  perm,
		SystemPrompt: "Test system prompt",
	})

	msgs := a.Messages()
	if len(msgs) < 1 {
		t.Fatal("expected at least 1 message (system prompt)")
	}
	if msgs[0].Role != "system" {
		t.Errorf("first message role should be %q, got %q", "system", msgs[0].Role)
	}
	if msgs[0].Content != "Test system prompt" {
		t.Errorf("first message content should be %q, got %q", "Test system prompt", msgs[0].Content)
	}
}

func TestAgentSaveLoadSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")

	reg := tools.NewRegistry()
	perm := permission.New(true)

	// Create agent and add some messages manually.
	a1 := New(Config{
		Provider:     &mockProvider{},
		Tools:        reg,
		Permissions:  perm,
		SystemPrompt: "System prompt",
	})

	// Simulate conversation by appending messages directly.
	a1.messages = append(a1.messages, provider.Message{
		Role:    "user",
		Content: "Hello",
	})
	a1.messages = append(a1.messages, provider.Message{
		Role:    "assistant",
		Content: "Hi there!",
	})

	if err := a1.SaveSession(sessionPath); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Verify file was written.
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("session file not created: %v", err)
	}

	// Load into a new agent.
	a2 := New(Config{
		Provider:     &mockProvider{},
		Tools:        reg,
		Permissions:  perm,
		SystemPrompt: "System prompt",
	})

	if err := a2.LoadSession(sessionPath); err != nil {
		t.Fatalf("load error: %v", err)
	}

	msgs := a2.Messages()
	// Should have: system + user + assistant = 3.
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[1].Content != "Hello" {
		t.Errorf("expected user message %q, got %q", "Hello", msgs[1].Content)
	}
	if msgs[2].Content != "Hi there!" {
		t.Errorf("expected assistant message %q, got %q", "Hi there!", msgs[2].Content)
	}
}

func TestAgentClearMessages(t *testing.T) {
	reg := tools.NewRegistry()
	perm := permission.New(true)

	a := New(Config{
		Provider:     &mockProvider{},
		Tools:        reg,
		Permissions:  perm,
		SystemPrompt: "System prompt",
	})

	// Add some messages.
	a.messages = append(a.messages, provider.Message{Role: "user", Content: "msg1"})
	a.messages = append(a.messages, provider.Message{Role: "assistant", Content: "msg2"})

	a.ClearMessages()

	msgs := a.Messages()
	if len(msgs) != 1 {
		t.Fatalf("after clear, expected 1 message (system), got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("remaining message should be system, got %q", msgs[0].Role)
	}
}
