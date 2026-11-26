package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// within fails the test if f does not return in time. The confirm channel in
// these tests is nil, so a wrongful prompt shows up as a hang.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked: the gate tried to prompt")
	}
}

// An unknown or empty permission mode must refuse. It used to fall through to
// "proceed", so a caller that set a bad mode ran everything unprompted.
func TestPolicyRejectsUnknownMode(t *testing.T) {
	for _, mode := range []string{"", "AUTO", "yolo", "nonsense"} {
		t.Run("native/"+mode, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "out.txt")
			m := &ChatModel{permissionMode: mode}
			var out string
			var isErr bool
			within(t, func() {
				out, isErr = m.makeExecutor()(context.Background(), "write_file",
					`{"path":"`+target+`","content":"x"}`)
			})
			if !isErr || !strings.Contains(out, "unknown permission mode") || !strings.Contains(out, "auto, confirm, suggest") {
				t.Errorf("got %q (isErr=%v), want a refusal naming the valid modes", out, isErr)
			}
			if _, err := os.Stat(target); err == nil {
				t.Error("the tool body ran under an unknown mode")
			}
		})
		t.Run("mcp/"+mode, func(t *testing.T) {
			mgr := &fakeMCP{readOnly: map[string]bool{}}
			m := &ChatModel{permissionMode: mode, mcpManager: mgr}
			var out string
			var isErr bool
			within(t, func() {
				out, isErr = m.makeExecutor()(context.Background(), "mcp__fs__write", "{}")
			})
			if !isErr || !strings.Contains(out, "unknown permission mode") {
				t.Errorf("got %q (isErr=%v), want a refusal", out, isErr)
			}
			if len(mgr.executed) != 0 {
				t.Errorf("the tool ran under an unknown mode: %v", mgr.executed)
			}
		})
	}
}

// Destructive meta-tools used their own copy of the switch, which also fell
// through on an unknown mode.
func TestMetaToolsShareGate(t *testing.T) {
	for _, mode := range []string{"", "AUTO", "yolo"} {
		ran := false
		var out string
		var isErr bool
		within(t, func() {
			out, isErr = executeDestructive(context.Background(), mode, nil, "commit_changes", "{}",
				func() (string, bool) {
					ran = true
					return "ran", false
				})
		})
		if ran || !isErr || !strings.Contains(out, "unknown permission mode") {
			t.Errorf("mode %q: got %q (isErr=%v, ran=%v), want a refusal", mode, out, isErr, ran)
		}
	}
}
