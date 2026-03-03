// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/logger"
	"github.com/arnelirobles/baryo-cli/internal/search"
)

// Meta-tool definitions for capable models (contextLimit >= 32K).
// These let the model decide when to search or research instead of
// relying on heuristic keyword matching.

var metaToolWebSearch = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "web_search",
		Description: "Search the web for current information. Use for recent events, current prices, news, or anything needing up-to-date data.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
	},
}

var metaToolDeepResearch = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "deep_research",
		Description: "Multi-round deep research. Use when user explicitly asks for research/investigation. Do NOT use for simple factual lookups.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"topic": map[string]interface{}{
					"type":        "string",
					"description": "The research topic",
				},
				"depth": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"quick", "standard", "deep"},
					"description": "Research depth: quick (1 round), standard (3 rounds), deep (5 rounds)",
				},
			},
			"required": []string{"topic"},
		},
	},
}

// MetaToolDefinitions returns tool definitions for web_search and deep_research.
func MetaToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{metaToolWebSearch, metaToolDeepResearch}
}

// IsMetaTool returns true if the tool name is a meta-tool handled by the executor.
func IsMetaTool(name string) bool {
	return name == "web_search" || name == "deep_research"
}

// makeMetaToolExecutor wraps a standard tool executor to intercept meta-tool
// calls (web_search, deep_research). All other tool names pass through.
func (m *ChatModel) makeMetaToolExecutor(inner llm.ToolExecutor) llm.ToolExecutor {
	ep := m.endpoint
	modelTag := m.modelTag
	provider := m.searchProvider
	apiKey := m.searchAPIKey
	contextLimit := m.contextLimit

	return func(ctx context.Context, name, argsJSON string) (string, bool) {
		switch name {
		case "web_search":
			return executeWebSearch(ctx, provider, apiKey, contextLimit, argsJSON)
		case "deep_research":
			return executeDeepResearch(ctx, provider, apiKey, ep, modelTag, contextLimit, argsJSON)
		default:
			return inner(ctx, name, argsJSON)
		}
	}
}

// executeWebSearch runs search.DeepQuery and truncates the result to fit context.
func executeWebSearch(ctx context.Context, provider, apiKey string, contextLimit int, argsJSON string) (string, bool) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), true
	}
	if args.Query == "" {
		return "missing required parameter: query", true
	}

	logger.Debug("meta-tool web_search", "query", args.Query)

	results, err := search.DeepQuery(provider, apiKey, args.Query)
	if err != nil {
		return fmt.Sprintf("search error: %v", err), true
	}

	// Truncate to ~25% of context window (in chars, using chars/4 token estimate).
	budget := contextLimit // contextLimit tokens * 4 chars/token * 25% ≈ contextLimit chars
	return search.TruncateContent(results, budget), false
}

// executeDeepResearch runs search.RunResearch synchronously and returns findings.
func executeDeepResearch(ctx context.Context, provider, apiKey string, ep llm.Endpoint, modelTag string, contextLimit int, argsJSON string) (string, bool) {
	var args struct {
		Topic string `json:"topic"`
		Depth string `json:"depth"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), true
	}
	if args.Topic == "" {
		return "missing required parameter: topic", true
	}

	depth := search.DepthStandard
	switch args.Depth {
	case "quick":
		depth = search.DepthQuick
	case "deep":
		depth = search.DepthDeep
	}

	logger.Debug("meta-tool deep_research", "topic", args.Topic, "depth", args.Depth)

	// Context budget: use ~60% of remaining context (rough estimate).
	contextBudget := contextLimit * 4 * 60 / 100 // tokens → chars, 60%
	if contextBudget < 4000 {
		contextBudget = 4000
	}

	cfg := search.ResearchConfig{
		Provider:      provider,
		APIKey:        apiKey,
		Topic:         args.Topic,
		Depth:         depth,
		ContextBudget: contextBudget,
		Progress:      nil, // tool loop doesn't support mid-execution updates
		ModelCall: func(callCtx context.Context, prompt string) (string, error) {
			msgs := []llm.ChatMessage{
				llm.NewChatMessage("user", prompt),
			}
			lowTemp := 0.3
			params := llm.ChatParams{Temperature: &lowTemp}
			ch := llm.StreamChat(callCtx, ep, modelTag, msgs, params)

			var result strings.Builder
			for evt := range ch {
				if evt.Error != "" {
					return result.String(), fmt.Errorf("%s", evt.Error)
				}
				if evt.Done {
					break
				}
				if evt.Token != "" {
					result.WriteString(evt.Token)
				}
			}
			return strings.TrimSpace(result.String()), nil
		},
	}

	res, err := search.RunResearch(ctx, cfg)
	if err != nil {
		return fmt.Sprintf("research error: %v", err), true
	}

	// Format output with sources.
	var b strings.Builder
	b.WriteString(res.AccumulatedContext)
	if len(res.Sources) > 0 {
		b.WriteString("\n\n## Sources\n")
		for _, s := range res.Sources {
			fmt.Fprintf(&b, "- [%s](%s)\n", s.Title, s.URL)
		}
	}

	return search.TruncateContent(b.String(), contextLimit*2), false
}
