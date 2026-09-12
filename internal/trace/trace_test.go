package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func read(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line is not JSON: %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func TestRecordsATaskAsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.trace.jsonl")
	r, err := New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.StartTask("fix the failing test", "qwen3", "local", "/repo", "abc123")
	r.ToolCall("read_file", `{"path":"main.go"}`)
	r.ToolResult("read_file", "package main", false)
	r.Verify("go test ./...", 0, "ok")
	r.EndTask("verified", 120, 45, 3*time.Second)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	recs := read(t, path)
	var types []string
	for _, rec := range recs {
		types = append(types, rec["t"].(string))
	}
	want := []string{"task_start", "tool_call", "tool_result", "verify", "task_end"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("record types = %v, want %v", types, want)
	}
	if recs[0]["prompt"] != "fix the failing test" {
		t.Errorf("task_start prompt = %v", recs[0]["prompt"])
	}
	for _, rec := range recs {
		if rec["task"] == "" || rec["task"] == nil {
			t.Errorf("record %v has no task id", rec["t"])
		}
	}
}

// Appending, never rewriting: a second task must not lose the first.
func TestAppendsAcrossTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s2.trace.jsonl")
	for i := 0; i < 2; i++ {
		r, err := New(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.StartTask("task", "m", "local", "/repo", "sha")
		r.EndTask("unknown", 0, 0, time.Second)
		r.Close()
	}
	if got := len(read(t, path)); got != 4 {
		t.Errorf("got %d records, want 4 across two tasks", got)
	}
}

// A trace is the richest secret-bearing artifact baryo writes, so known secrets
// and key-shaped tokens must not reach the file.
func TestRedactsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s3.trace.jsonl")
	r, err := New(path, []string{"super-secret-key-value"})
	if err != nil {
		t.Fatal(err)
	}
	r.StartTask("deploy", "m", "local", "/repo", "sha")
	r.ToolCall("shell", `{"cmd":"curl -H 'Authorization: Bearer super-secret-key-value' x"}`)
	r.ToolResult("shell", "token sk-abcdefghijklmnopqrstuvwxyz012345 and ghp_abcdefghijklmnopqrstuvwxyz0123", false)
	r.EndTask("unknown", 0, 0, time.Second)
	r.Close()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{
		"super-secret-key-value",
		"sk-abcdefghijklmnopqrstuvwxyz012345",
		"ghp_abcdefghijklmnopqrstuvwxyz0123",
	} {
		if strings.Contains(string(body), leak) {
			t.Errorf("trace leaked %q", leak)
		}
	}
	if !strings.Contains(string(body), "[redacted]") {
		t.Error("nothing was redacted, so the redactor did not run")
	}
}

func TestCapsLargeContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s4.trace.jsonl")
	r, _ := New(path, nil)
	r.StartTask("big", "m", "local", "/repo", "sha")
	r.ToolResult("read_file", strings.Repeat("x", 200_000), false)
	r.EndTask("unknown", 0, 0, time.Second)
	r.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 64_000 {
		t.Errorf("trace is %d bytes: large tool output is not capped", info.Size())
	}
	if !strings.Contains(string(mustRead(t, path)), "truncated") {
		t.Error("truncation is not recorded, so a reader cannot tell")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Call sites should not need nil checks.
func TestNilRecorderIsSafe(t *testing.T) {
	var r *Recorder
	r.StartTask("x", "m", "local", "/repo", "sha")
	r.ToolCall("read_file", "{}")
	r.ToolResult("read_file", "y", false)
	r.Verify("go test", 1, "fail")
	r.Diff("--- a\n+++ b\n")
	r.EndTask("unknown", 0, 0, time.Second)
	if err := r.Close(); err != nil {
		t.Errorf("Close on a nil recorder: %v", err)
	}
}

// The executor can be called from more than one goroutine, so lines must not
// interleave.
func TestConcurrentWritesStayWellFormed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s5.trace.jsonl")
	r, _ := New(path, nil)
	r.StartTask("concurrent", "m", "local", "/repo", "sha")

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.ToolCall("read_file", `{"path":"a.go"}`)
			r.ToolResult("read_file", strings.Repeat("y", 200), false)
		}(i)
	}
	wg.Wait()
	r.Close()

	if got := len(read(t, path)); got != 81 {
		t.Errorf("got %d well-formed records, want 81", got)
	}
}
