package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Headless has no prompt. In confirm mode a call that needs approval is
// refused at once, it does not wait for an answer that cannot come.
func TestHeadlessConfirmModeRefusesWithoutBlocking(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out.txt")
	mgr := &fakeMCP{readOnly: map[string]bool{}}
	exec := makeHeadlessExecutor("confirm", mgr, nil)

	calls := map[string]string{
		"write_file":     `{"path":"` + target + `","content":"x"}`,
		"mcp__fs__write": "{}",
	}
	for name, args := range calls {
		name, args := name, args
		done := make(chan string, 1)
		go func() {
			out, isErr := exec(context.Background(), name, args)
			if !isErr {
				out = "not refused: " + out
			}
			done <- out
		}()
		select {
		case out := <-done:
			if !strings.Contains(out, "--yolo") {
				t.Errorf("%s: got %q, want a refusal naming --yolo", name, out)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: headless executor blocked waiting for approval", name)
		}
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("write_file ran without approval")
	}
	if len(mgr.executed) != 0 {
		t.Errorf("MCP tool ran without approval: %v", mgr.executed)
	}
}

// An unknown mode refuses in headless too.
func TestHeadlessUnknownModeRefuses(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{}}
	for _, mode := range []string{"", "AUTO", "yolo"} {
		out, isErr := makeHeadlessExecutor(mode, mgr, nil)(context.Background(), "mcp__fs__write", "{}")
		if !isErr || !strings.Contains(out, "unknown permission mode") {
			t.Errorf("mode %q: got %q (isErr=%v), want a refusal naming the mode", mode, out, isErr)
		}
	}
	if len(mgr.executed) != 0 {
		t.Errorf("tool ran under an unknown mode: %v", mgr.executed)
	}
}
