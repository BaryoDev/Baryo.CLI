// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Plan mode does not offer the destructive meta-tools, but a model can still
// emit their names. Under permission mode auto they used to run.
func TestReadOnlyMetaExecutorRefusesDestructiveTools(t *testing.T) {
	t.Chdir(t.TempDir()) // never run a real commit against this repo
	m := &ChatModel{permissionMode: "auto"}
	innerCalled := false
	inner := func(ctx context.Context, name, args string) (string, bool) {
		innerCalled = true
		return "inner", false
	}
	exec := m.makeMetaToolExecutor(inner, true)

	for _, name := range []string{"commit_changes", "create_pr", "create_branch", "run_tests"} {
		out, isErr := exec(context.Background(), name, `{"message":"x","title":"x","name":"x"}`)
		if !isErr || !strings.Contains(out, "[read-only mode]") {
			t.Errorf("%s: got %q (isErr=%v), want a read-only mode refusal", name, out, isErr)
		}
	}
	if innerCalled {
		t.Error("destructive meta-tool reached the inner executor")
	}
}

func TestMetaExecutorPassesThroughNonMetaTools(t *testing.T) {
	m := &ChatModel{permissionMode: "auto"}
	exec := m.makeMetaToolExecutor(func(ctx context.Context, name, args string) (string, bool) {
		return "inner", false
	}, true)
	if out, _ := exec(context.Background(), "read_file", "{}"); out != "inner" {
		t.Errorf("non-meta tools must pass through, got %q", out)
	}
}

// A confirm request on a full channel with a cancelled context must return,
// not block the tool loop forever.
func TestDestructiveConfirmSendHonoursCancel(t *testing.T) {
	ch := make(chan confirmRequest, 1)
	ch <- confirmRequest{} // fill the buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan string, 1)
	go func() {
		out, _ := executeDestructive(ctx, "confirm", ch, "commit_changes", `{}`, func() (string, bool) {
			return "ran", false
		})
		done <- out
	}()
	select {
	case out := <-done:
		if out != "cancelled" {
			t.Errorf("got %q, want cancelled", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executeDestructive blocked on a full channel after cancel")
	}
}
