// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/logger"
	"github.com/arnelirobles/baryo-cli/internal/tools"
)

const (
	subagentTimeout    = 5 * time.Minute
	subagentMaxRounds  = 5
	maxActiveSubagents = 3
)

// SubagentState tracks a running or completed subagent.
type SubagentState struct {
	ID          int
	Description string
	Status      string // "running", "completed", "failed"
	StartTime   time.Time
	Result      string
	Cancel      context.CancelFunc
}

// SubagentConfig holds all parameters needed to run a subagent.
type SubagentConfig struct {
	ID            int
	Description   string
	Endpoint      llm.Endpoint
	ModelTag      string
	SystemPrompt  string
	TokenBudget   int
	MCPManager    MCPManager
	MCPInReadOnly bool
}

// SubagentResult is the outcome of a subagent run.
type SubagentResult struct {
	ID          int
	Description string
	Content     string
	Err         error
	Elapsed     time.Duration
}

// RunSubagent runs an isolated sub-task with read-only tools and returns the result.
func RunSubagent(ctx context.Context, cfg SubagentConfig, progressCh chan<- SubagentProgressMsg) SubagentResult {
	start := time.Now()
	logger.Debug("subagent start", "id", cfg.ID, "desc", cfg.Description)

	// Build isolated message history
	messages := []llm.ChatMessage{
		llm.NewChatMessage("system", cfg.SystemPrompt+"\n\nYou are a focused sub-task agent. Complete the following task concisely. You have read-only access to tools. Do not ask follow-up questions — just produce the best result you can."),
		llm.NewChatMessage("user", cfg.Description),
	}

	// Read-only tool definitions + executor
	toolDefs := tools.ReadOnlyDockerDefinitions()
	executor := makeSubagentExecutor(cfg.MCPManager, cfg.MCPInReadOnly)

	if cfg.MCPManager != nil && cfg.MCPInReadOnly {
		toolDefs = append(toolDefs, cfg.MCPManager.CompactToolDefinitions(tools.Names(), 0)...)
	}

	// Stream with tool loop
	ch := llm.StreamChatWithToolsN(ctx, cfg.Endpoint, cfg.ModelTag, messages, llm.ChatParams{}, toolDefs, executor, subagentMaxRounds)

	var content strings.Builder
	for evt := range ch {
		if evt.Error != "" {
			return SubagentResult{
				ID:          cfg.ID,
				Description: cfg.Description,
				Content:     content.String(),
				Err:         fmt.Errorf("%s", evt.Error),
				Elapsed:     time.Since(start),
			}
		}
		if evt.Token != "" {
			content.WriteString(evt.Token)
		}
		if evt.ToolStart != nil {
			sendSubagentProgress(ctx, progressCh, SubagentProgressMsg{
				ID:     cfg.ID,
				Status: fmt.Sprintf("Running %s...", evt.ToolStart.Name),
			})
		}
	}

	logger.Debug("subagent done", "id", cfg.ID, "elapsed", time.Since(start))
	return SubagentResult{
		ID:          cfg.ID,
		Description: cfg.Description,
		Content:     content.String(),
		Elapsed:     time.Since(start),
	}
}

// makeSubagentExecutor returns a read-only tool executor for subagents.
func makeSubagentExecutor(mgr MCPManager, allowMCP bool) llm.ToolExecutor {
	return func(ctx context.Context, name, argsJSON string) (string, bool) {
		// Route MCP tools if allowed
		if mgr != nil && mgr.IsMCPTool(name) {
			if !allowMCP {
				return fmt.Sprintf("[subagent] MCP tool %s not available in read-only mode", name), true
			}
			return mgr.Execute(ctx, name, argsJSON)
		}

		// Block destructive tools
		if tools.IsDestructive(name) {
			return fmt.Sprintf("[subagent] %s blocked — subagents have read-only access", name), true
		}

		r := tools.Execute(ctx, name, argsJSON)
		return r.Content, r.IsError
	}
}

func sendSubagentProgress(ctx context.Context, ch chan<- SubagentProgressMsg, msg SubagentProgressMsg) {
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	case <-ctx.Done():
	default:
	}
}
