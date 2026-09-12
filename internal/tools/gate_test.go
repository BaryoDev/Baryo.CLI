package tools

import "testing"

// Every caller of IsDestructive uses it as a permission gate: chat.go:3223,
// chat.go:3278, subagent.go:125 and print.go:305. A name the registry has never
// heard of must therefore be treated as dangerous, not as safe.
func TestIsDestructiveFailsClosedForUnknownTool(t *testing.T) {
	if !IsDestructive("tool_that_does_not_exist") {
		t.Error("an unregistered tool name must be treated as destructive")
	}
}

func TestIsDestructiveForRegisteredTools(t *testing.T) {
	if IsDestructive("read_file") {
		t.Error("read_file is not destructive")
	}
	if !IsDestructive("write_file") {
		t.Error("write_file is destructive")
	}
}

func TestExists(t *testing.T) {
	if !Exists("read_file") {
		t.Error("read_file is registered")
	}
	if Exists("tool_that_does_not_exist") {
		t.Error("an unregistered name must not report as existing")
	}
}
