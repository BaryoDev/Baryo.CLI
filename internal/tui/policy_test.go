package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/trace"
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

// Every mode against every tool kind. P is proceed, Q is prompt, R is refuse.
func TestPolicyGateMatrix(t *testing.T) {
	const (
		P = Proceed
		Q = Prompt
		R = Refuse
	)
	// Columns: read-only native, destructive native, MCP read-only,
	// MCP unmarked, meta-tool.
	kinds := []ToolKind{KindReadOnly, KindDestructive, KindMCPReadOnly, KindMCPUnmarked, KindMeta}

	cases := []struct {
		name   string
		policy Policy
		want   map[string][5]Action
	}{
		{"interactive", Policy{CanPrompt: true, Words: interactiveWords}, map[string][5]Action{
			"auto":    {P, P, P, P, P},
			"confirm": {P, Q, P, Q, Q},
			"suggest": {P, R, P, R, R},
			"":        {P, R, P, R, R},
			"AUTO":    {P, R, P, R, R},
			"yolo":    {P, R, P, R, R},
		}},
		{"headless", Policy{Words: headlessWords}, map[string][5]Action{
			"auto":    {P, P, P, P, P},
			"confirm": {P, R, P, R, R},
			"suggest": {P, R, P, R, R},
			"":        {P, R, P, R, R},
			"yolo":    {P, R, P, R, R},
		}},
		{"read-only", Policy{ReadOnly: true, Words: planWords}, map[string][5]Action{
			"auto":    {P, R, R, R, R},
			"confirm": {P, R, R, R, R},
			"":        {P, R, R, R, R},
		}},
		{"read-only with MCP", Policy{ReadOnly: true, AllowMCP: true, Words: planWords}, map[string][5]Action{
			"auto":    {P, R, P, R, R},
			"confirm": {P, R, P, R, R},
			"":        {P, R, P, R, R},
		}},
	}
	for _, c := range cases {
		for mode, want := range c.want {
			for i, kind := range kinds {
				p := c.policy
				p.Mode = mode
				got := decide(p, kind, "tool")
				if got.Action != want[i] {
					t.Errorf("%s mode %q kind %d: got action %d, want %d", c.name, mode, kind, got.Action, want[i])
				}
				if got.Action == Refuse && got.Msg == "" {
					t.Errorf("%s mode %q kind %d: refusal has no message", c.name, mode, kind)
				}
			}
		}
	}
}

func hookPolicy(t *testing.T, mode string, runHooks bool) (Policy, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	return Policy{
		Mode:     mode,
		Words:    interactiveWords,
		Hooks:    config.HooksConfig{PreTool: "touch " + marker + "; exit 1"},
		RunHooks: runHooks,
	}, marker
}

// Where hooks run, they run before the gate. A blocking hook wins over a
// refusal from the gate, and the hook runs even when the gate would refuse.
func TestExecuteHooksOrder(t *testing.T) {
	p, marker := hookPolicy(t, "suggest", true)
	var out string
	var isErr bool
	within(t, func() {
		out, isErr = Execute(context.Background(), p, nil, "write_file", `{"path":"x","content":"y"}`)
	})
	if !isErr || !strings.Contains(out, "[hook blocked]") {
		t.Errorf("got %q (isErr=%v), want the hook to block before the gate", out, isErr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the pre_tool hook did not run")
	}

	// A policy with hooks off ignores the configured hook.
	p, marker = hookPolicy(t, "suggest", false)
	out, isErr = Execute(context.Background(), p, nil, "write_file", `{"path":"x","content":"y"}`)
	if !isErr || !strings.Contains(out, "suggest mode") {
		t.Errorf("got %q (isErr=%v), want the gate's refusal", out, isErr)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the hook ran under a policy with hooks off")
	}

	// An unknown tool is rejected before the hook runs.
	p, marker = hookPolicy(t, "auto", true)
	out, _ = Execute(context.Background(), p, nil, "tool_that_does_not_exist", "{}")
	if !strings.Contains(out, "unknown tool") {
		t.Errorf("got %q, want unknown tool", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the hook ran for an unknown tool")
	}
}

func traceLines(t *testing.T, rec *trace.Recorder, path string) []string {
	t.Helper()
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// A refused call is not recorded. An allowed call records one tool_call and
// one tool_result.
func TestExecuteRecordsAllowedCallsOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	file := "in.txt"
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "refused.jsonl")
	rec, err := trace.New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{Mode: "suggest", Words: interactiveWords}
	if _, isErr := Execute(context.Background(), p, rec, "write_file", `{"path":"`+"out.txt"+`","content":"y"}`); !isErr {
		t.Fatal("want a refusal")
	}
	if _, isErr := Execute(context.Background(), p, rec, "tool_that_does_not_exist", "{}"); !isErr {
		t.Fatal("want a refusal")
	}
	if got := traceLines(t, rec, path); len(got) != 1 || got[0] != "" {
		t.Errorf("refused calls were recorded: %v", got)
	}

	path = filepath.Join(dir, "allowed.jsonl")
	rec, err = trace.New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out, isErr := Execute(context.Background(), p, rec, "read_file", `{"path":"`+file+`"}`); isErr {
		t.Fatalf("read_file failed: %q", out)
	}
	got := traceLines(t, rec, path)
	if len(got) != 2 || !strings.Contains(got[0], `"tool_call"`) || !strings.Contains(got[1], `"tool_result"`) {
		t.Errorf("got %v, want one tool_call then one tool_result", got)
	}
}

// No refusal message may be built from an empty format, for any word set,
// mode or kind, including a policy with zero-value Words.
func TestPolicyRefusalsAreWellFormed(t *testing.T) {
	sets := map[string]Words{
		"zero": {}, "interactive": interactiveWords, "plan": planWords,
		"subagent": subagentWords, "headless": headlessWords,
	}
	kinds := []ToolKind{KindReadOnly, KindDestructive, KindMCPReadOnly, KindMCPUnmarked, KindMeta}
	for setName, w := range sets {
		for _, mode := range []string{"auto", "confirm", "suggest", "", "yolo"} {
			for _, readOnly := range []bool{false, true} {
				for _, canPrompt := range []bool{false, true} {
					for _, kind := range kinds {
						p := Policy{Mode: mode, ReadOnly: readOnly, CanPrompt: canPrompt, Words: w}
						d := decide(p, kind, "tool")
						if d.Action == Refuse && (strings.Contains(d.Msg, "%!") || !strings.Contains(d.Msg, "tool")) {
							t.Errorf("%s mode %q readOnly=%v canPrompt=%v kind %d: bad message %q", setName, mode, readOnly, canPrompt, kind, d.Msg)
						}
					}
				}
			}
		}
	}
}
