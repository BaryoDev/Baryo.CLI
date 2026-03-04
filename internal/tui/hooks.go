// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/logger"
)

// HookEvent identifies the lifecycle event that triggered a hook.
type HookEvent string

const (
	HookPreTool     HookEvent = "pre_tool"
	HookPostTool    HookEvent = "post_tool"
	HookOnError     HookEvent = "on_error"
	HookOnCommit    HookEvent = "on_commit"
	HookOnStreamEnd HookEvent = "on_stream_end"
	HookOnSearch    HookEvent = "on_search"

	hookTimeout       = 30 * time.Second
	hookOutputLimit   = 2000
	hookEnvValueLimit = 4000
)

// HookContext provides event-specific data to hook commands via env vars.
type HookContext struct {
	ToolName  string // BARYO_HOOK_TOOL_NAME
	Output    string // BARYO_HOOK_OUTPUT
	Error     string // BARYO_HOOK_ERROR
	Query     string // BARYO_HOOK_QUERY
	CommitMsg string // BARYO_HOOK_COMMIT_MSG
}

// HookResult holds the outcome of a hook execution.
type HookResult struct {
	Event   HookEvent
	Output  string
	Err     error
	Blocked bool // true if pre_tool hook exited non-zero (cancels the tool)
}

// hookCommand returns the shell command string for the given event, or "" if none.
func hookCommand(hooks config.HooksConfig, event HookEvent) string {
	switch event {
	case HookPreTool:
		return hooks.PreTool
	case HookPostTool:
		return hooks.PostTool
	case HookOnError:
		return hooks.OnError
	case HookOnCommit:
		return hooks.OnCommit
	case HookOnStreamEnd:
		return hooks.OnStreamEnd
	case HookOnSearch:
		return hooks.OnSearch
	}
	return ""
}

// runHook executes a hook shell command synchronously with a timeout.
// Returns a HookResult with output (truncated to hookOutputLimit) and error status.
func runHook(hooks config.HooksConfig, event HookEvent, hctx HookContext) HookResult {
	cmd := hookCommand(hooks, event)
	if cmd == "" {
		return HookResult{Event: event}
	}

	logger.Debug("hook run", "event", string(event), "command", cmd)

	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()

	proc := exec.CommandContext(ctx, "sh", "-c", cmd)
	proc.Env = append(proc.Environ(),
		"BARYO_HOOK_EVENT="+string(event),
		"BARYO_HOOK_TOOL_NAME="+truncateEnv(hctx.ToolName),
		"BARYO_HOOK_OUTPUT="+truncateEnv(hctx.Output),
		"BARYO_HOOK_ERROR="+truncateEnv(hctx.Error),
		"BARYO_HOOK_QUERY="+truncateEnv(hctx.Query),
		"BARYO_HOOK_COMMIT_MSG="+truncateEnv(hctx.CommitMsg),
	)

	var buf bytes.Buffer
	proc.Stdout = &buf
	proc.Stderr = &buf

	err := proc.Run()
	output := truncateOutput(buf.String())

	result := HookResult{
		Event:  event,
		Output: output,
		Err:    err,
	}
	if event == HookPreTool && err != nil {
		result.Blocked = true
	}

	logger.Debug("hook done", "event", string(event), "blocked", result.Blocked, "err", err)
	return result
}

// fireHookCmd returns a tea.Cmd that runs a hook asynchronously and sends a HookResultMsg.
func fireHookCmd(hooks config.HooksConfig, event HookEvent, hctx HookContext) tea.Cmd {
	if hookCommand(hooks, event) == "" {
		return nil
	}
	return func() tea.Msg {
		return HookResultMsg(runHook(hooks, event, hctx))
	}
}

func truncateEnv(s string) string {
	if len(s) > hookEnvValueLimit {
		return s[:hookEnvValueLimit]
	}
	return s
}

func truncateOutput(s string) string {
	if len(s) > hookOutputLimit {
		return s[:hookOutputLimit]
	}
	return s
}
