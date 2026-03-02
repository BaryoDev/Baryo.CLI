// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// strategyPhase tracks the current state of a /strategy session.
type strategyPhase int

const (
	strategyIdle      strategyPhase = iota
	strategyGathering               // wizard: model asking questions
	strategyDone                    // analysis produced, refinement possible
)

// StrategyGoal describes the decision or outcome the user wants to achieve.
type StrategyGoal struct {
	Description string `json:"description"`
	TimeHorizon string `json:"time_horizon,omitempty"`
	Priority    string `json:"priority,omitempty"`
}

// StrategyFact is a piece of information relevant to the decision.
type StrategyFact struct {
	Category    string `json:"category"`
	Description string `json:"description"`
	Confidence  string `json:"confidence,omitempty"`
}

// StrategyConstraint is a hard limit or non-negotiable requirement.
type StrategyConstraint struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Flexibility string `json:"flexibility,omitempty"`
}

// StrategyContext provides optional background for the decision.
type StrategyContext struct {
	Domain     string `json:"domain,omitempty"`
	Background string `json:"background,omitempty"`
}

// StrategyInput is the complete structured input for strategy analysis.
type StrategyInput struct {
	Goal        StrategyGoal         `json:"goal"`
	Facts       []StrategyFact       `json:"facts"`
	Constraints []StrategyConstraint `json:"constraints"`
	Context     *StrategyContext     `json:"context,omitempty"`
}

// handleStrategy is the main dispatcher for /strategy commands.
func (m ChatModel) handleStrategy(arg string) (ChatModel, tea.Cmd) {
	arg = strings.TrimSpace(arg)

	switch {
	case arg == "":
		return m.handleStrategyWizard()
	case strings.EqualFold(arg, "init"):
		return m.handleStrategyInit()
	case strings.EqualFold(arg, "done"):
		return m.handleStrategyDone()
	default:
		return m.handleStrategyFile(arg)
	}
}

// handleStrategyWizard starts the interactive gathering mode.
func (m ChatModel) handleStrategyWizard() (ChatModel, tea.Cmd) {
	if m.strategyPhase == strategyGathering {
		m.history = append(m.history, chatEntry{
			role:    roleAssistant,
			content: "Already in strategy gathering mode. Answer the questions or say \"done\" to analyze.",
		})
		m.updateViewport()
		return m, nil
	}

	m.strategyPhase = strategyGathering
	m.history = append(m.history, chatEntry{
		role:    roleTool,
		content: "Starting strategy wizard...",
	})

	// Inject the gather prompt as a hidden user message
	m.messages = append(m.messages, llm.NewChatMessage("user", "[strategy wizard]\n\n"+strategyGatherPrompt))

	// Start no-tool stream so model asks the first question
	m.isStream = true
	m.streaming = ""
	m.turnContent = ""
	m.toolStatus = ""
	m.streamStart = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel
	m.eventCh = llm.StreamChat(ctx, m.endpoint, m.modelTag, m.buildMessagesWithToolGating(false), m.params)

	m.updateViewport()
	return m, tea.Batch(waitForEvent(m.eventCh), doSpinTick())
}

// handleStrategyFile reads and parses a JSON strategy file.
func (m ChatModel) handleStrategyFile(path string) (ChatModel, tea.Cmd) {
	m.history = append(m.history, chatEntry{
		role:    roleTool,
		content: fmt.Sprintf("Loading strategy from %s...", path),
	})
	m.updateViewport()

	return m, func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return StrategyLoadedMsg{Err: fmt.Errorf("cannot read %s: %w", path, err), Path: path}
		}

		var input StrategyInput
		if err := json.Unmarshal(data, &input); err != nil {
			return StrategyLoadedMsg{Err: fmt.Errorf("invalid JSON in %s: %w", path, err), Path: path}
		}

		goal, facts, constraints, context := FormatStrategyInput(input)
		return StrategyLoadedMsg{
			Goal:        goal,
			Facts:       facts,
			Constraints: constraints,
			Context:     context,
			Path:        path,
		}
	}
}

// handleStrategyInit writes a blank template to strategy.json.
func (m ChatModel) handleStrategyInit() (ChatModel, tea.Cmd) {
	blank := StrategyInput{
		Goal: StrategyGoal{
			Description: "Describe your goal here",
			TimeHorizon: "e.g. 6 months, 1 year, 5 years",
			Priority:    "e.g. high, medium, low",
		},
		Facts: []StrategyFact{
			{Category: "financial", Description: "e.g. budget is $50k", Confidence: "high"},
			{Category: "personal", Description: "e.g. 5 years experience in marketing", Confidence: "high"},
		},
		Constraints: []StrategyConstraint{
			{Type: "budget", Description: "e.g. cannot exceed $50k", Flexibility: "none"},
			{Type: "time", Description: "e.g. must decide by March", Flexibility: "some"},
		},
		Context: &StrategyContext{
			Domain:     "e.g. career, marketing, health, education, finance",
			Background: "Optional background information about the situation",
		},
	}

	data, err := json.MarshalIndent(blank, "", "  ")
	if err != nil {
		m.history = append(m.history, chatEntry{
			role:    roleError,
			content: fmt.Sprintf("Failed to create template: %v", err),
		})
		m.updateViewport()
		return m, nil
	}

	if err := os.WriteFile("strategy.json", data, 0644); err != nil {
		m.history = append(m.history, chatEntry{
			role:    roleError,
			content: fmt.Sprintf("Failed to write strategy.json: %v", err),
		})
		m.updateViewport()
		return m, nil
	}

	m.history = append(m.history, chatEntry{
		role:    roleAssistant,
		content: "Template saved to strategy.json — edit it, then run /strategy strategy.json",
	})
	m.updateViewport()
	return m, nil
}

// handleStrategyDone exits strategy mode.
func (m ChatModel) handleStrategyDone() (ChatModel, tea.Cmd) {
	if m.strategyPhase == strategyIdle {
		m.history = append(m.history, chatEntry{
			role:    roleAssistant,
			content: "Not in strategy mode.",
		})
		m.updateViewport()
		return m, nil
	}

	m.strategyPhase = strategyIdle
	m.strategyContext = ""
	m.history = append(m.history, chatEntry{
		role:    roleAssistant,
		content: "Exited strategy mode.",
	})
	m.updateViewport()
	return m, nil
}

// FormatStrategyInput converts a StrategyInput into readable prompt text.
// Exported so print.go can reuse it.
func FormatStrategyInput(input StrategyInput) (goal, facts, constraints, context string) {
	// Goal
	goal = input.Goal.Description
	if input.Goal.TimeHorizon != "" {
		goal += fmt.Sprintf(" (time horizon: %s)", input.Goal.TimeHorizon)
	}
	if input.Goal.Priority != "" {
		goal += fmt.Sprintf(" [priority: %s]", input.Goal.Priority)
	}

	// Facts
	var fb strings.Builder
	for i, f := range input.Facts {
		fmt.Fprintf(&fb, "F%d [%s]: %s", i+1, f.Category, f.Description)
		if f.Confidence != "" {
			fmt.Fprintf(&fb, " (confidence: %s)", f.Confidence)
		}
		fb.WriteString("\n")
	}
	facts = strings.TrimSpace(fb.String())

	// Constraints
	var cb strings.Builder
	for i, c := range input.Constraints {
		fmt.Fprintf(&cb, "C%d [%s]: %s", i+1, c.Type, c.Description)
		if c.Flexibility != "" {
			fmt.Fprintf(&cb, " (flexibility: %s)", c.Flexibility)
		}
		cb.WriteString("\n")
	}
	constraints = strings.TrimSpace(cb.String())

	// Context
	if input.Context != nil {
		var ctxb strings.Builder
		if input.Context.Domain != "" {
			fmt.Fprintf(&ctxb, "Domain: %s\n", input.Context.Domain)
		}
		if input.Context.Background != "" {
			fmt.Fprintf(&ctxb, "Background: %s", input.Context.Background)
		}
		context = strings.TrimSpace(ctxb.String())
	}
	if context == "" {
		context = "(none provided)"
	}

	return goal, facts, constraints, context
}

// isStrategyDoneSignal detects when the user wants to trigger the analysis.
func isStrategyDoneSignal(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	signals := []string{"done", "analyze", "go", "that's all", "thats all", "let's go", "lets go", "ready", "proceed"}
	return slices.Contains(signals, lower)
}
