package tools

import (
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &BashTool{}
	r.Register(tool)

	got, err := r.Get("bash")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name() != "bash" {
		t.Errorf("expected name %q, got %q", "bash", got.Name())
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent tool, got nil")
	}
}

func TestRegistryDefaults(t *testing.T) {
	r := NewRegistry()
	RegisterDefaults(r)

	all := r.All()
	if len(all) < 9 {
		t.Errorf("RegisterDefaults should register at least 9 tools, got %d", len(all))
	}
}

func TestRegistryToolDefs(t *testing.T) {
	r := NewRegistry()
	RegisterDefaults(r)

	defs := r.Defs()
	for _, d := range defs {
		if d.Function.Name == "" {
			t.Error("tool def has empty name")
		}
		if d.Function.Description == "" {
			t.Errorf("tool %q has empty description", d.Function.Name)
		}
		if d.Type != "function" {
			t.Errorf("tool %q has type %q, want %q", d.Function.Name, d.Type, "function")
		}
	}
}
