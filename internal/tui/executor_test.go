package tui

import (
	"context"
	"strings"
	"testing"
	"time"
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
