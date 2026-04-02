package tools

import (
	"strings"
	"testing"
)

func TestBashEcho(t *testing.T) {
	b := &BashTool{}
	out, err := b.Execute(map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain %q, got %q", "hello", out)
	}
}

func TestBashTimeout(t *testing.T) {
	b := &BashTool{}
	_, err := b.Execute(map[string]any{
		"command": "sleep 10",
		"timeout": float64(1),
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestBashBlocklist(t *testing.T) {
	b := &BashTool{}

	dangerous := []string{
		"rm -rf /",
		"rm -rf /*",
		"sudo rm -rf /home",
		"mkfs.ext4 /dev/sda",
		"dd if=/dev/zero of=/dev/sda",
		"chmod -R 777 /",
	}

	for _, cmd := range dangerous {
		_, err := b.Execute(map[string]any{
			"command": cmd,
		})
		if err == nil {
			t.Errorf("expected blocked error for %q, got nil", cmd)
		}
		if err != nil && !strings.Contains(err.Error(), "blocked") {
			t.Errorf("expected blocked error for %q, got: %v", cmd, err)
		}
	}
}

func TestBashOutputCap(t *testing.T) {
	b := &BashTool{}
	// Generate output larger than 128KB.
	out, err := b.Execute(map[string]any{
		"command": "python3 -c \"print('x' * 200000)\"",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) > maxOutputSize+100 {
		t.Errorf("output should be capped near %d bytes, got %d", maxOutputSize, len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Error("expected truncation notice in output")
	}
}

func TestBashMissingCommand(t *testing.T) {
	b := &BashTool{}
	_, err := b.Execute(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
}
