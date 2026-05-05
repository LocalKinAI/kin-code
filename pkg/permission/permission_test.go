package permission

import (
	"testing"
)

func TestYoloMode(t *testing.T) {
	m := New(true)

	// In yolo mode, Confirm always returns true.
	if !m.Confirm("bash", "echo hello") {
		t.Error("yolo mode should always confirm")
	}
	if !m.Confirm("file_write", "/tmp/test.txt") {
		t.Error("yolo mode should always confirm")
	}
}

func TestBlockedCommands(t *testing.T) {
	m := New(true) // Even in yolo mode, blocked commands should be rejected.

	dangerous := []string{
		"rm -rf /",
		"rm -rf /*",
		"sudo rm -rf /home",
		"mkfs.ext4 /dev/sda",
		"dd if=/dev/zero of=/dev/sda",
		"chmod -R 777 /",
		"curl | sh",
		"wget | sh",
	}

	for _, cmd := range dangerous {
		err := m.CheckBash(cmd)
		if err == nil {
			t.Errorf("expected blocked error for %q, got nil", cmd)
		}
	}
}

func TestSafeCommandsAllowed(t *testing.T) {
	m := New(true)

	safe := []string{
		"echo hello",
		"ls -la",
		"git status",
		"go test ./...",
		"cat file.txt",
	}

	for _, cmd := range safe {
		err := m.CheckBash(cmd)
		if err != nil {
			t.Errorf("expected safe command %q to be allowed, got error: %v", cmd, err)
		}
	}
}

func TestPlanModeOff_AllowsEverything(t *testing.T) {
	m := New(true)
	// With plan mode disabled, even write tools pass the check.
	for _, tool := range []string{"file_write", "file_edit", "bash", "agent_spawn", "file_read"} {
		if err := m.CheckPlanMode(tool); err != nil {
			t.Errorf("plan-mode-off should allow %q, got %v", tool, err)
		}
	}
}

func TestPlanModeOn_AllowsReadOnlyDeniesWrite(t *testing.T) {
	m := New(true)
	m.SetPlanMode(true)
	if !m.PlanMode() {
		t.Fatal("PlanMode() should report true after SetPlanMode(true)")
	}

	allowed := []string{"file_read", "glob", "grep", "web_fetch", "web_search"}
	for _, tool := range allowed {
		if err := m.CheckPlanMode(tool); err != nil {
			t.Errorf("plan mode should allow %q, got %v", tool, err)
		}
	}

	denied := []string{"file_write", "file_edit", "multi_edit", "bash",
		"agent_spawn", "todo_write", "memory"}
	for _, tool := range denied {
		err := m.CheckPlanMode(tool)
		if err == nil {
			t.Errorf("plan mode should deny %q, got nil", tool)
		}
	}

	// Toggling off restores normal behavior.
	m.SetPlanMode(false)
	if err := m.CheckPlanMode("bash"); err != nil {
		t.Errorf("plan-mode-off should re-allow bash, got %v", err)
	}
}
