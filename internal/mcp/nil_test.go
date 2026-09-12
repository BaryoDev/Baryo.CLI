package mcp

import (
	"context"
	"testing"
)

// A *Manager stored in an interface field is not a nil interface, so callers
// that check `!= nil` still reach these methods. They must all survive a nil
// receiver, because main.go declares `var mcpMgr *mcp.Manager` and assigns it
// to an interface whether or not any server was configured.
func TestNilManagerMethodsDoNotPanic(t *testing.T) {
	var m *Manager

	if got := m.ServerNames(); got != nil {
		t.Errorf("ServerNames on nil = %v, want nil", got)
	}
	if got := m.CompactToolDefinitions([]string{"read_file"}, 32000); got != nil {
		t.Errorf("CompactToolDefinitions on nil = %v, want nil", got)
	}
	if got := m.ToolDefinitions(); got != nil {
		t.Errorf("ToolDefinitions on nil = %v, want nil", got)
	}
	if m.IsMCPTool("mcp__x__y") {
		t.Error("IsMCPTool on nil should be false")
	}
	if m.IsReadOnlyTool("mcp__x__y") {
		t.Error("IsReadOnlyTool on nil should be false")
	}
	if got := m.ServerTools("x"); got != nil {
		t.Errorf("ServerTools on nil = %v, want nil", got)
	}
	if _, isErr := m.Execute(context.Background(), "mcp__x__y", "{}"); !isErr {
		t.Error("Execute on nil should report an error, not succeed")
	}
	m.Close()
}
