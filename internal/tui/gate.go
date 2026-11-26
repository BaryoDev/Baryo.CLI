// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"

	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/tools"
	"github.com/arnelirobles/baryo-cli/internal/trace"
)

// validModes lists the permission modes the gate understands.
const validModes = "auto, confirm, suggest"

// unknownModeMessage is the refusal for a mode the gate does not know. An
// unknown or empty mode must never run a call that needs approval.
func unknownModeMessage(mode, name string) string {
	return fmt.Sprintf("[blocked] %q not run, unknown permission mode %q (valid modes: %s)", name, mode, validModes)
}

// ToolKind is what the gate needs to know about a call.
type ToolKind int

const (
	KindUnknown     ToolKind = iota // zero value, always refused
	KindReadOnly                    // native tool that does not write
	KindDestructive                 // native tool that writes
	KindMCPReadOnly                 // MCP tool known to be read-only
	KindMCPUnmarked                 // MCP tool not known to be read-only
	KindMeta                        // destructive meta-tool
)

// Action is the gate's answer.
type Action int

const (
	Proceed Action = iota
	Prompt
	Refuse
)

// Decision is an Action plus, for Refuse, the message to return to the model.
type Decision struct {
	Action Action
	Msg    string
}

// Words holds the refusal wording of one policy. Each field is a format string
// that takes the tool name.
type Words struct {
	Suggest     string // suggest mode
	SuggestMCP  string // suggest mode, MCP tool (falls back to Suggest)
	NoPrompt    string // approval needed but no prompt is possible
	NoPromptMCP string // same, for an MCP tool
	ReadOnly    string // destructive call in a read-only policy
	MCPOff      string // MCP tool in a read-only policy that disallows MCP
	MCPUnmarked string // MCP tool not known read-only, read-only policy
}

// MCPRouter is the part of an MCP manager the executor needs.
type MCPRouter interface {
	IsMCPTool(name string) bool
	IsReadOnlyTool(name string) bool
	Execute(ctx context.Context, qualifiedName, argsJSON string) (string, bool)
}

// Policy is everything that differs between the TUI, plan mode, subagents and
// headless. The gate and Execute are the same for all of them.
type Policy struct {
	Mode      string // auto, confirm or suggest; anything else refuses
	ReadOnly  bool   // refuse every call that is not read-only
	AllowMCP  bool   // ReadOnly only: let known read-only MCP tools run
	CanPrompt bool   // a person can approve a call
	MCP       MCPRouter
	Hooks     config.HooksConfig // pre and post tool hooks, used when RunHooks is set
	RunHooks  bool
	AutoFix   *AutoFixConfig // nil turns the auto check off
	Words     Words

	confirm chan confirmRequest
}

var interactiveWords = Words{
	Suggest: "[suggest mode] Would run %q, approve with --yolo or permission_mode: auto",
}

var planWords = Words{
	ReadOnly:    "[read-only mode] %q is not available, only read-only tools are allowed",
	MCPOff:      "[read-only mode] %q is not available, MCP tools are disabled in read-only modes (set mcp_in_read_only: true to allow)",
	MCPUnmarked: "[read-only mode] %q is not available, it is not marked read-only by its server (set trust: read-only on that server to allow)",
}

var subagentWords = Words{
	ReadOnly:    "[subagent] %q blocked, subagents have read-only access",
	MCPOff:      "[subagent] MCP tool %q not available in read-only mode",
	MCPUnmarked: "[subagent] MCP tool %q is not marked read-only by its server",
}

const headlessNoPrompt = "tool %q requires --yolo flag for headless execution"

const headlessNoPromptMCP = "MCP tool %q is not marked read-only and requires --yolo flag for headless execution"

var headlessWords = Words{
	Suggest:     headlessNoPrompt,
	NoPrompt:    headlessNoPrompt,
	NoPromptMCP: headlessNoPromptMCP,
	SuggestMCP:  headlessNoPromptMCP,
}

// HeadlessPolicy is the policy for print mode: no prompt, no hooks.
func HeadlessPolicy(mode string, mcp MCPRouter) Policy {
	return Policy{Mode: mode, MCP: mcp, Words: headlessWords}
}

// decide is the whole permission switch. It is pure: it never prompts or runs
// anything.
func decide(p Policy, kind ToolKind, name string) Decision {
	switch kind {
	case KindReadOnly, KindDestructive, KindMCPReadOnly, KindMCPUnmarked, KindMeta:
	default:
		return refuse("[blocked] %q not run, unknown tool kind", name)
	}
	if p.ReadOnly {
		switch kind {
		case KindReadOnly:
			return Decision{}
		case KindMCPReadOnly, KindMCPUnmarked:
			if !p.AllowMCP {
				return refuse(p.Words.MCPOff, name)
			}
			if kind == KindMCPUnmarked {
				return refuse(p.Words.MCPUnmarked, name)
			}
			return Decision{}
		default:
			return refuse(p.Words.ReadOnly, name)
		}
	}
	if kind == KindReadOnly || kind == KindMCPReadOnly {
		return Decision{}
	}
	noPrompt := noPromptFormat(p, kind)
	switch p.Mode {
	case "auto":
		return Decision{}
	case "suggest":
		format := p.Words.Suggest
		if kind == KindMCPUnmarked && p.Words.SuggestMCP != "" {
			format = p.Words.SuggestMCP
		}
		return refuse(format, name)
	case "confirm":
		if !p.CanPrompt {
			return refuse(noPrompt, name)
		}
		return Decision{Action: Prompt}
	}
	return Decision{Action: Refuse, Msg: unknownModeMessage(p.Mode, name)}
}

// noPromptFormat is the wording for a call that needs approval when no prompt
// is possible.
func noPromptFormat(p Policy, kind ToolKind) string {
	if kind == KindMCPUnmarked && p.Words.NoPromptMCP != "" {
		return p.Words.NoPromptMCP
	}
	return p.Words.NoPrompt
}

func refuse(format, name string) Decision {
	if format == "" {
		format = "[blocked] %q needs approval and no prompt is possible here"
	}
	return Decision{Action: Refuse, Msg: fmt.Sprintf(format, name)}
}

// gate applies decide and, for a Prompt, asks the user. It returns a message
// and true when the call must not proceed.
func gate(ctx context.Context, p Policy, kind ToolKind, name, argsJSON string) (string, bool) {
	d := decide(p, kind, name)
	switch d.Action {
	case Refuse:
		return d.Msg, true
	case Prompt:
		if p.confirm == nil {
			// Nobody can be asked, so waiting would only hang.
			return refuse(noPromptFormat(p, kind), name).Msg, true
		}
		respCh := make(chan bool, 1)
		select {
		case p.confirm <- confirmRequest{
			Name:   name,
			Args:   argsJSON,
			Prompt: formatConfirmPrompt(name, argsJSON),
			RespCh: respCh,
		}:
		case <-ctx.Done():
			return "cancelled", true
		}
		select {
		case approved := <-respCh:
			if !approved {
				return fmt.Sprintf("[denied] %q was not approved", name), true
			}
		case <-ctx.Done():
			return "cancelled", true
		}
	}
	return "", false
}

// confirmGate applies the permission mode to a call that needs approval in the
// TUI. It returns a message and true when the call must not proceed.
func confirmGate(ctx context.Context, mode string, ch chan confirmRequest, name, argsJSON string) (string, bool) {
	p := Policy{Mode: mode, CanPrompt: true, Words: interactiveWords, confirm: ch}
	return gate(ctx, p, KindDestructive, name, argsJSON)
}

// classify maps a tool name to its kind.
func classify(p Policy, name string) (kind ToolKind, known bool) {
	if p.MCP != nil && p.MCP.IsMCPTool(name) {
		if p.MCP.IsReadOnlyTool(name) {
			return KindMCPReadOnly, true
		}
		return KindMCPUnmarked, true
	}
	if !tools.Exists(name) {
		return 0, false
	}
	if tools.IsDestructive(name) {
		return KindDestructive, true
	}
	return KindReadOnly, true
}

// Execute runs one tool call under a policy. Order: unknown names are
// rejected, the pre_tool hook runs, the gate decides, the call is recorded
// and run, then auto-fix and the post_tool hook. A nil recorder records
// nothing. A refused call is never recorded.
func Execute(ctx context.Context, p Policy, rec *trace.Recorder, name, argsJSON string) (string, bool) {
	kind, known := classify(p, name)
	if !known {
		return fmt.Sprintf("unknown tool: %q", name), true
	}
	body := func() (string, bool) {
		if kind == KindMCPReadOnly || kind == KindMCPUnmarked {
			return p.MCP.Execute(ctx, name, argsJSON)
		}
		r := tools.Execute(ctx, name, argsJSON)
		return r.Content, r.IsError
	}
	var after func(string, bool) string
	if kind != KindMCPReadOnly && kind != KindMCPUnmarked && p.AutoFix != nil {
		after = func(content string, isErr bool) string {
			if !isErr && isCodeModifyingTool(name) {
				if checkOutput := runAutoCheck(ctx, *p.AutoFix); checkOutput != "" {
					content += "\n\n" + checkOutput
				}
			}
			return content
		}
	}
	return runGated(ctx, p, rec, kind, name, argsJSON, body, after)
}

// executeMeta runs a destructive meta-tool through the same hooks, gate and
// recorder as Execute. Meta-tools have no auto-fix.
func executeMeta(ctx context.Context, p Policy, rec *trace.Recorder, name, argsJSON string, fn func() (string, bool)) (string, bool) {
	return runGated(ctx, p, rec, KindMeta, name, argsJSON, fn, nil)
}

// runGated is the shared path: pre hook, gate, record, body, optional
// after step, post hook. after sees the recorded result and may extend it.
func runGated(ctx context.Context, p Policy, rec *trace.Recorder, kind ToolKind, name, argsJSON string, body func() (string, bool), after func(string, bool) string) (string, bool) {
	if p.RunHooks && p.Hooks.PreTool != "" {
		hr := runHook(p.Hooks, HookPreTool, HookContext{ToolName: name})
		if hr.Blocked {
			msg := fmt.Sprintf("[hook blocked] %q was cancelled by pre_tool hook", name)
			if hr.Output != "" {
				msg += ": " + hr.Output
			}
			return msg, true
		}
	}

	if msg, blocked := gate(ctx, p, kind, name, argsJSON); blocked {
		return msg, true
	}

	rec.ToolCall(name, argsJSON)
	content, isErr := body()
	rec.ToolResult(name, content, isErr)
	if after != nil {
		content = after(content, isErr)
	}
	if p.RunHooks && p.Hooks.PostTool != "" {
		hr := runHook(p.Hooks, HookPostTool, HookContext{ToolName: name, Output: content})
		if hr.Output != "" {
			content += "\n\n[post_tool hook]\n" + hr.Output
		}
	}
	return content, isErr
}
