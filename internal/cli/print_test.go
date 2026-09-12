package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// Headless mode blocks destructive tools without --yolo. Since IsDestructive
// fails closed, an unregistered name would be reported as needing --yolo, which
// is misleading: no flag can make a tool that does not exist run.
func TestHeadlessExecutorRejectsUnknownTool(t *testing.T) {
	out, isErr := makeHeadlessExecutor("confirm", nil)(context.Background(), "tool_that_does_not_exist", "{}")
	if !isErr {
		t.Error("want an error result")
	}
	if !strings.Contains(out, "unknown tool") {
		t.Errorf("got %q, want it to name the tool as unknown", out)
	}
}

type fakeMCP struct {
	readOnly map[string]bool
	executed []string
}

func (f *fakeMCP) CompactToolDefinitions([]string, int) []llm.ToolDefinition { return nil }
func (f *fakeMCP) Execute(_ context.Context, name, _ string) (string, bool) {
	f.executed = append(f.executed, name)
	return "mcp ok", false
}
func (f *fakeMCP) IsMCPTool(name string) bool      { return strings.HasPrefix(name, "mcp__") }
func (f *fakeMCP) IsReadOnlyTool(name string) bool { return f.readOnly[name] }

// Headless blocks native destructive tools without --yolo. MCP tools used to
// skip that check entirely.
func TestHeadlessExecutorGatesNonReadOnlyMCPTool(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{"mcp__search__query": true}}
	exec := makeHeadlessExecutor("confirm", mgr)

	out, isErr := exec(context.Background(), "mcp__fs__write", "{}")
	if !isErr || !strings.Contains(out, "--yolo") {
		t.Errorf("got %q (isErr=%v), want it blocked pending --yolo", out, isErr)
	}
	if len(mgr.executed) != 0 {
		t.Errorf("tool ran while blocked: %v", mgr.executed)
	}

	if out, isErr := exec(context.Background(), "mcp__search__query", "{}"); isErr {
		t.Errorf("read-only MCP tool should run in headless mode, got %q", out)
	}
}

func TestHeadlessExecutorRunsAnyMCPToolInAutoMode(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{}}
	if out, isErr := makeHeadlessExecutor("auto", mgr)(context.Background(), "mcp__fs__write", "{}"); isErr {
		t.Errorf("auto mode should run it, got %q", out)
	}
}
