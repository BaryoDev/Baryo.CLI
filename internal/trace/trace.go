// SPDX-License-Identifier: MIT

// Package trace records what a task actually did: the tool calls, their
// arguments and results, any diff, and how the task was verified.
//
// This exists because the saved conversation does not contain it. After a tool
// loop the TUI keeps only the concatenated narration, so the actions and their
// results are executed and discarded. Without a trace there is nothing to
// distill a reusable procedure from.
//
// Two rules shape the design:
//
//   - A trace is never read back into the prompt. The message history *is* the
//     prompt, and folding tool results into it would change what every later
//     turn sends to the model and can push real context off the end of a small
//     window. This is a parallel sink.
//   - Records are appended, never rewritten, so a long session does not
//     rewrite a growing file on every turn.
package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Caps per record. A trace is a diagnostic artifact, not an archive: one
// read_file of a large source file should not dominate it.
const (
	maxArgs    = 4 << 10
	maxContent = 8 << 10
	maxDiff    = 64 << 10
	maxOutput  = 8 << 10
)

// secretPatterns match token shapes that must never be written out, regardless
// of whether the value is known to us.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password)"?\s*[:=]\s*"?([A-Za-z0-9_\-\.]{12,})`),
}

// Recorder appends records for one session. All methods are safe on a nil
// receiver so call sites do not need a check.
type Recorder struct {
	mu      sync.Mutex
	f       *os.File
	secrets []string
	taskID  string
}

// New opens (or creates) a trace file for appending. secrets are exact values,
// typically provider API keys, that must be masked wherever they appear.
func New(path string, secrets []string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	kept := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if len(s) >= 8 {
			kept = append(kept, s)
		}
	}
	return &Recorder{f: f, secrets: kept}, nil
}

// StartTask begins a task and returns its id.
func (r *Recorder) StartTask(prompt, model, endpoint, cwd, head string) string {
	if r == nil {
		return ""
	}
	id := time.Now().UTC().Format("20060102T150405.000000000")
	r.mu.Lock()
	r.taskID = id
	r.mu.Unlock()
	r.write(map[string]any{
		"t":        "task_start",
		"prompt":   r.clean(prompt, maxContent),
		"model":    model,
		"endpoint": endpoint,
		"cwd":      cwd,
		"head":     head,
	})
	return id
}

// ToolCall records a tool invocation and its arguments.
func (r *Recorder) ToolCall(name, argsJSON string) {
	r.write(map[string]any{"t": "tool_call", "name": name, "args": r.clean(argsJSON, maxArgs)})
}

// ToolResult records what a tool returned.
func (r *Recorder) ToolResult(name, content string, isError bool) {
	r.write(map[string]any{
		"t":        "tool_result",
		"name":     name,
		"bytes":    len(content),
		"is_error": isError,
		"content":  r.clean(content, maxContent),
	})
}

// Diff records the unified diff a task produced, which is what a judge needs in
// order to judge anything.
func (r *Recorder) Diff(unified string) {
	r.write(map[string]any{"t": "diff", "unified": r.clean(unified, maxDiff)})
}

// Verify records a machine check and its exit code. This is the only evidence
// that can promote a procedure on its own.
func (r *Recorder) Verify(command string, exitCode int, output string) {
	r.write(map[string]any{
		"t":         "verify",
		"command":   r.clean(command, maxArgs),
		"exit_code": exitCode,
		"output":    r.clean(output, maxOutput),
	})
}

// EndTask closes out a task.
func (r *Recorder) EndTask(outcome string, promptTokens, completionTokens int, wall time.Duration) {
	r.write(map[string]any{
		"t":       "task_end",
		"outcome": outcome,
		"usage": map[string]int{
			"prompt":     promptTokens,
			"completion": completionTokens,
		},
		"wall_ms": wall.Milliseconds(),
	})
}

// Close releases the file.
func (r *Recorder) Close() error {
	if r == nil || r.f == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.f.Close()
	r.f = nil
	return err
}

// write appends one record. One lock around marshal and write, so records from
// concurrent tool calls cannot interleave.
func (r *Recorder) write(rec map[string]any) {
	if r == nil || r.f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return
	}
	rec["task"] = r.taskID
	rec["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	r.f.Write(append(line, '\n'))
}

// clean redacts secrets and caps length, noting when it truncated.
func (r *Recorder) clean(s string, max int) string {
	if r != nil {
		for _, secret := range r.secrets {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[redacted]")
	}
	if len(s) > max {
		s = s[:max] + "\n... (truncated)"
	}
	return s
}
