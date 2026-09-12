package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// IsDestructive fails closed, so an unregistered name now looks destructive. In
// confirm mode that would make the executor ask the user to approve a tool that
// does not exist and cannot run. The executor must reject the name instead.
//
// confirmCh is nil here: if the executor tries to prompt, the send blocks
// forever and this test times out, which is exactly the bug.
func TestExecutorRejectsUnknownToolWithoutPrompting(t *testing.T) {
	m := &ChatModel{permissionMode: "confirm"}
	exec := m.makeExecutor()

	done := make(chan string, 1)
	go func() {
		out, _ := exec(context.Background(), "tool_that_does_not_exist", "{}")
		done <- out
	}()

	select {
	case out := <-done:
		if !strings.Contains(out, "unknown tool") {
			t.Errorf("got %q, want it to name the tool as unknown", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor blocked: it tried to confirm an unregistered tool name")
	}
}

func TestPlanExecutorRejectsUnknownTool(t *testing.T) {
	m := &ChatModel{}
	out, isErr := m.makePlanExecutor()(context.Background(), "tool_that_does_not_exist", "{}")
	if !isErr || !strings.Contains(out, "unknown tool") {
		t.Errorf("got %q (isErr=%v), want an unknown-tool error", out, isErr)
	}
}

func TestSubagentExecutorRejectsUnknownTool(t *testing.T) {
	out, isErr := makeSubagentExecutor(nil, false)(context.Background(), "tool_that_does_not_exist", "{}")
	if !isErr || !strings.Contains(out, "unknown tool") {
		t.Errorf("got %q (isErr=%v), want an unknown-tool error", out, isErr)
	}
}

// fakeMCP implements MCPManager. readOnly lists the qualified names it reports
// as read-only; executed records calls that reached Execute.
type fakeMCP struct {
	readOnly map[string]bool
	executed []string
}

func (f *fakeMCP) ToolDefinitions() []llm.ToolDefinition { return nil }
func (f *fakeMCP) CompactToolDefinitions([]string, int) []llm.ToolDefinition {
	return nil
}
func (f *fakeMCP) Execute(_ context.Context, name, _ string) (string, bool) {
	f.executed = append(f.executed, name)
	return "mcp ok", false
}
func (f *fakeMCP) IsMCPTool(name string) bool { return strings.HasPrefix(name, "mcp__") }
func (f *fakeMCP) IsReadOnlyTool(name string) bool {
	return f.readOnly[name]
}
func (f *fakeMCP) ServerNames() []string       { return nil }
func (f *fakeMCP) ServerTools(string) []string { return nil }
func (f *fakeMCP) Close()                      {}

// A third-party MCP tool that is not known to be read-only must pass the same
// gate as a native destructive tool. It used to bypass the gate entirely.
func TestExecutorGatesNonReadOnlyMCPTool(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{}}
	ch := make(chan confirmRequest, 1)
	m := &ChatModel{permissionMode: "confirm", mcpManager: mgr, confirmCh: ch}

	go func() {
		req := <-ch
		req.RespCh <- false // deny
	}()

	out, isErr := m.makeExecutor()(context.Background(), "mcp__fs__write", `{"path":"x"}`)
	if !isErr || !strings.Contains(out, "denied") {
		t.Errorf("got %q (isErr=%v), want a denial", out, isErr)
	}
	if len(mgr.executed) != 0 {
		t.Errorf("tool ran despite denial: %v", mgr.executed)
	}
}

// A tool the server annotated read-only, or whose server is trusted read-only,
// must not prompt. confirmCh is nil, so a prompt would hang.
func TestExecutorDoesNotGateReadOnlyMCPTool(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{"mcp__search__query": true}}
	m := &ChatModel{permissionMode: "confirm", mcpManager: mgr}

	done := make(chan string, 1)
	go func() {
		out, _ := m.makeExecutor()(context.Background(), "mcp__search__query", "{}")
		done <- out
	}()

	select {
	case out := <-done:
		if out != "mcp ok" {
			t.Errorf("got %q, want the tool to run", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor prompted for a read-only MCP tool")
	}
}

func TestPlanExecutorAllowsOnlyReadOnlyMCPTools(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{"mcp__search__query": true}}
	m := &ChatModel{mcpManager: mgr, mcpInReadOnly: true}
	exec := m.makePlanExecutor()

	if out, isErr := exec(context.Background(), "mcp__search__query", "{}"); isErr {
		t.Errorf("read-only MCP tool should be allowed in read-only mode, got %q", out)
	}
	out, isErr := exec(context.Background(), "mcp__fs__write", "{}")
	if !isErr || !strings.Contains(out, "read-only mode") {
		t.Errorf("got %q (isErr=%v), want a read-only mode refusal", out, isErr)
	}
	if len(mgr.executed) != 1 {
		t.Errorf("expected exactly the read-only tool to run, got %v", mgr.executed)
	}
}

// Subagents are read-only by design, so the same rule applies to their MCP access.
func TestSubagentExecutorAllowsOnlyReadOnlyMCPTools(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{"mcp__search__query": true}}
	exec := makeSubagentExecutor(mgr, true)

	if out, isErr := exec(context.Background(), "mcp__search__query", "{}"); isErr {
		t.Errorf("read-only MCP tool should be allowed, got %q", out)
	}
	if out, isErr := exec(context.Background(), "mcp__fs__write", "{}"); !isErr {
		t.Errorf("got %q, want a write tool to be blocked for a subagent", out)
	}
	if len(mgr.executed) != 1 {
		t.Errorf("expected only the read-only tool to run, got %v", mgr.executed)
	}
}
