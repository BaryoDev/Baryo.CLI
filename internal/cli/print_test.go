package cli

import (
	"context"
	"strings"
	"testing"
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
