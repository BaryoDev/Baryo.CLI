// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

// AgentMode represents a specialized operating mode for the chat session.
type AgentMode string

const (
	ModeChat      AgentMode = "chat"
	ModeAsk       AgentMode = "ask"
	ModeCode      AgentMode = "code"
	ModeArchitect AgentMode = "architect"
	ModeReview    AgentMode = "review"
	ModeResearch  AgentMode = "research"
)

// ToolStrategy determines how tools are provisioned in a given mode.
type ToolStrategy int

const (
	ToolsDynamic  ToolStrategy = iota // needsTools() heuristic (default chat behavior)
	ToolsNone                         // no tools at all
	ToolsAll                          // full tool access on every message
	ToolsReadOnly                     // read-only tools only
)

// ModeConfig holds the static configuration for an agent mode.
type ModeConfig struct {
	Label       string       // display label for status bar
	Description string       // shown in /mode list
	Tools       ToolStrategy // tool provisioning strategy
	Prompt      string       // mode-specific system prompt (set by initModePrompts)
}

// modeRegistry maps each mode to its configuration.
var modeRegistry = map[AgentMode]ModeConfig{
	ModeChat: {
		Label:       "chat",
		Description: "Default mode — dynamic tool access",
		Tools:       ToolsDynamic,
	},
	ModeAsk: {
		Label:       "ask",
		Description: "No tools, fast answers",
		Tools:       ToolsNone,
	},
	ModeCode: {
		Label:       "code",
		Description: "Full tool access on every message",
		Tools:       ToolsAll,
	},
	ModeArchitect: {
		Label:       "architect",
		Description: "Read-only tools — explore and plan",
		Tools:       ToolsReadOnly,
	},
	ModeReview: {
		Label:       "review",
		Description: "Read-only tools — code review focus",
		Tools:       ToolsReadOnly,
	},
	ModeResearch: {
		Label:       "research",
		Description: "Read-only tools — web search and exploration",
		Tools:       ToolsReadOnly,
	},
}

// modeOrder defines the display order for AllModes().
var modeOrder = []AgentMode{
	ModeChat, ModeAsk, ModeCode, ModeArchitect, ModeReview, ModeResearch,
}

// initModePrompts sets the Prompt field for each mode from the embedded prompt files.
// Called from init() in chat.go after embed vars are populated.
func initModePrompts() {
	set := func(mode AgentMode, prompt string) {
		cfg := modeRegistry[mode]
		cfg.Prompt = prompt
		modeRegistry[mode] = cfg
	}
	set(ModeAsk, askModePrompt)
	set(ModeCode, codeModePrompt)
	set(ModeArchitect, planModePrompt)
	set(ModeReview, reviewModePrompt)
	set(ModeResearch, researchModePrompt)
	// ModeChat has no extra prompt — uses base system prompt only.
}

// LookupMode finds a mode by name (case-insensitive).
func LookupMode(name string) (AgentMode, ModeConfig, bool) {
	for _, mode := range modeOrder {
		if string(mode) == name {
			cfg := modeRegistry[mode]
			return mode, cfg, true
		}
	}
	return "", ModeConfig{}, false
}

// AllModes returns all modes in display order.
func AllModes() []AgentMode {
	return modeOrder
}
