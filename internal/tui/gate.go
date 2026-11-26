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
	return fmt.Sprintf("[blocked] %s not run, unknown permission mode %q (valid modes: %s)", name, mode, validModes)
}

// ToolKind is what the gate needs to know about a call.
type ToolKind int

const (
	KindReadOnly    ToolKind = iota // native tool that does not write
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
	Suggest     string // suggest mode, when a prompt is possible
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
	Suggest: "[suggest mode] Would run %s, approve with --yolo or permission_mode: auto",
}

var planWords = Words{
	ReadOnly:    "[read-only mode] %s is not available, only read-only tools are allowed",
	MCPOff:      "[read-only mode] %s is not available, MCP tools are disabled in read-only modes (set mcp_in_read_only: true to allow)",
	MCPUnmarked: "[read-only mode] %s is not available, it is not marked read-only by its server (set trust: read-only on that server to allow)",
}

var subagentWords = Words{
	ReadOnly:    "[subagent] %s blocked, subagents have read-only access",
	MCPOff:      "[subagent] MCP tool %s not available in read-only mode",
	MCPUnmarked: "[subagent] MCP tool %s is not marked read-only by its server",
}

const headlessNoPrompt = "tool %q requires --yolo flag for headless execution"

var headlessWords = Words{
	Suggest:     headlessNoPrompt,
	NoPrompt:    headlessNoPrompt,
	NoPromptMCP: "MCP tool %q is not marked read-only and requires --yolo flag for headless execution",
}

// HeadlessPolicy is the policy for print mode: no prompt, no hooks.
func HeadlessPolicy(mode string, mcp MCPRouter) Policy {
	return Policy{Mode: mode, MCP: mcp, Words: headlessWords}
}

// decide is the whole permission switch. It is pure: it never prompts or runs
// anything.
func decide(p Policy, kind ToolKind, name string) Decision {
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
	noPrompt := p.Words.NoPrompt
	if kind == KindMCPUnmarked && p.Words.NoPromptMCP != "" {
		noPrompt = p.Words.NoPromptMCP
	}
	switch p.Mode {
	case "auto":
		return Decision{}
	case "suggest":
		if !p.CanPrompt {
			return refuse(noPrompt, name)
		}
		return refuse(p.Words.Suggest, name)
	case "confirm":
		if !p.CanPrompt {
			return refuse(noPrompt, name)
		}
		return Decision{Action: Prompt}
	}
	return Decision{Action: Refuse, Msg: unknownModeMessage(p.Mode, name)}
}

func refuse(format, name string) Decision {
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
				return fmt.Sprintf("[denied] %s was not approved", name), true
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
		return fmt.Sprintf("unknown tool: %s", name), true
	}
	isMCP := kind == KindMCPReadOnly || kind == KindMCPUnmarked

	if p.RunHooks && p.Hooks.PreTool != "" {
		hr := runHook(p.Hooks, HookPreTool, HookContext{ToolName: name})
		if hr.Blocked {
			msg := fmt.Sprintf("[hook blocked] %s was cancelled by pre_tool hook", name)
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
	var content string
	var isErr bool
	if isMCP {
		content, isErr = p.MCP.Execute(ctx, name, argsJSON)
		rec.ToolResult(name, content, isErr)
	} else {
		r := tools.Execute(ctx, name, argsJSON)
		content, isErr = r.Content, r.IsError
		rec.ToolResult(name, content, isErr)
		if !isErr && p.AutoFix != nil && isCodeModifyingTool(name) {
			if checkOutput := runAutoCheck(ctx, *p.AutoFix); checkOutput != "" {
				content += "\n\n" + checkOutput
			}
		}
	}
	if p.RunHooks && p.Hooks.PostTool != "" {
		hr := runHook(p.Hooks, HookPostTool, HookContext{ToolName: name, Output: content})
		if hr.Output != "" {
			content += "\n\n[post_tool hook]\n" + hr.Output
		}
	}
	return content, isErr
}
