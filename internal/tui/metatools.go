// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/logger"
	"github.com/arnelirobles/baryo-cli/internal/search"
)

// Meta-tool definitions for capable models (contextLimit >= 32K).
// These let the model decide when to search, fetch, commit, etc. instead of
// relying on heuristic keyword matching or requiring slash commands.

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

var metaToolFetchPage = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "fetch_page",
		Description: "Fetch and extract text content from a URL. Use to follow links from search results or when user shares a URL.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to fetch",
				},
			},
			"required": []string{"url"},
		},
	},
}

var metaToolRemember = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "remember",
		Description: "Save a user preference or important fact to persistent memory. Only use when the user explicitly asks you to remember something.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"fact": map[string]interface{}{
					"type":        "string",
					"description": "The fact or preference to remember",
				},
			},
			"required": []string{"fact"},
		},
	},
}

var metaToolReviewCode = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "review_code",
		Description: "Get the current git diff for code review. Use when user asks to review changes, check work, or see what's modified.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	},
}

var metaToolCommitChanges = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "commit_changes",
		Description: "Stage all changes and commit with an auto-generated conventional commit message. Only use when user explicitly asks to commit.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Optional commit message. If empty, one is auto-generated from the diff.",
				},
			},
		},
	},
}

var metaToolCreatePR = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "create_pr",
		Description: "Push current branch and create a GitHub pull request. Requires gh CLI. Only use when user explicitly asks to create a PR.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "PR title. If empty, one is auto-generated from branch name and commits.",
				},
			},
		},
	},
}

var metaToolReviewPR = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "review_pr",
		Description: "Fetch a GitHub PR (diff + comments) for code review. Use when user asks to review a specific pull request.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"number": map[string]interface{}{
					"type":        "string",
					"description": "PR number to review. If empty, reviews the latest PR.",
				},
			},
		},
	},
}

var metaToolReadIssue = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "read_issue",
		Description: "Read a GitHub issue (body + comments). Use when user mentions an issue number or asks about issues.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"number": map[string]interface{}{
					"type":        "string",
					"description": "Issue number to read.",
				},
			},
			"required": []string{"number"},
		},
	},
}

var metaToolPRStatus = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "pr_status",
		Description: "Show PR review status for the current repository. Use when user asks about PR status or pending reviews.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	},
}

var metaToolCreateBranch = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "create_branch",
		Description: "Create and checkout a new git branch. Use when starting work on a new feature or issue.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Branch name (e.g. feat/add-auth, fix/login-bug).",
				},
			},
			"required": []string{"name"},
		},
	},
}

var metaToolRunTests = llm.ToolDefinition{
	Type: "function",
	Function: llm.FunctionDefinition{
		Name:        "run_tests",
		Description: "Auto-detect the project's test framework and run tests. Only use when user explicitly asks to run or check tests.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional path to test (e.g. './pkg/...' or 'tests/'). If empty, runs all tests.",
				},
			},
		},
	},
}

// metaToolRegistry maps tool names to whether they exist as meta-tools.
var metaToolRegistry = map[string]bool{
	"web_search":     true,
	"deep_research":  true,
	"fetch_page":     true,
	"remember":       true,
	"review_code":    true,
	"commit_changes": true,
	"create_pr":      true,
	"run_tests":      true,
	"review_pr":      true,
	"read_issue":     true,
	"pr_status":      true,
	"create_branch":  true,
}

// MetaToolDefinitions returns safe meta-tool definitions (usable in all modes including read-only).
func MetaToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		metaToolWebSearch,
		metaToolDeepResearch,
		metaToolFetchPage,
		metaToolRemember,
		metaToolReviewCode,
		metaToolReviewPR,
		metaToolReadIssue,
		metaToolPRStatus,
	}
}

// DestructiveMetaToolDefinitions returns meta-tools that modify state (commit, PR, tests).
// These should only be added in non-read-only modes.
func DestructiveMetaToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		metaToolCommitChanges,
		metaToolCreatePR,
		metaToolRunTests,
		metaToolCreateBranch,
	}
}

// IsMetaTool returns true if the tool name is a meta-tool handled by the executor.
func IsMetaTool(name string) bool {
	return metaToolRegistry[name]
}

// MetaToolsSystemPrompt returns the embedded metatools.md content for injection
// into the system prompt when meta-tools are enabled.
func MetaToolsSystemPrompt() string {
	return metaToolsPrompt
}

// HeadlessMetaToolOpts holds configuration for the headless meta-tool executor.
type HeadlessMetaToolOpts struct {
	Endpoint       llm.Endpoint
	ModelTag       string
	SearchProvider string
	SearchAPIKey   string
	ContextLimit   int
	PermissionMode string
}

// MakeHeadlessMetaToolExecutor wraps a standard tool executor to intercept
// meta-tool calls in headless (print) mode. Destructive tools are gated by
// PermissionMode: only "auto" allows execution; all other modes block.
func MakeHeadlessMetaToolExecutor(inner llm.ToolExecutor, opts HeadlessMetaToolOpts) llm.ToolExecutor {
	return func(ctx context.Context, name, argsJSON string) (string, bool) {
		switch name {
		case "web_search":
			return executeWebSearch(ctx, opts.SearchProvider, opts.SearchAPIKey, opts.ContextLimit, argsJSON)
		case "deep_research":
			return executeDeepResearch(ctx, opts.SearchProvider, opts.SearchAPIKey, opts.Endpoint, opts.ModelTag, opts.ContextLimit, argsJSON)
		case "fetch_page":
			return executeFetchPage(ctx, opts.ContextLimit, argsJSON)
		case "remember":
			return executeRemember(ctx, argsJSON)
		case "review_code":
			return executeReviewCode(ctx, opts.ContextLimit)
		case "commit_changes":
			return headlessDestructiveGate(opts.PermissionMode, name, func() (string, bool) {
				return executeCommitChanges(ctx, argsJSON)
			})
		case "create_pr":
			return headlessDestructiveGate(opts.PermissionMode, name, func() (string, bool) {
				return executeCreatePR(ctx, argsJSON)
			})
		case "run_tests":
			return headlessDestructiveGate(opts.PermissionMode, name, func() (string, bool) {
				return executeRunTests(ctx, argsJSON)
			})
		case "review_pr":
			return executeReviewPR(ctx, opts.ContextLimit, argsJSON)
		case "read_issue":
			return executeReadIssue(ctx, opts.ContextLimit, argsJSON)
		case "pr_status":
			return executePRStatus(ctx, argsJSON)
		case "create_branch":
			return headlessDestructiveGate(opts.PermissionMode, name, func() (string, bool) {
				return executeCreateBranch(ctx, argsJSON)
			})
		default:
			return inner(ctx, name, argsJSON)
		}
	}
}

// headlessDestructiveGate blocks destructive tools unless PermissionMode is "auto".
// Headless mode has no confirmation dialog, so non-auto modes simply block.
func headlessDestructiveGate(mode, name string, fn func() (string, bool)) (string, bool) {
	if mode != "auto" {
		return fmt.Sprintf("tool %q requires --yolo flag for headless execution", name), true
	}
	return fn()
}

// makeMetaToolExecutor wraps a standard tool executor to intercept meta-tool
// calls. All other tool names pass through to the inner executor.
func (m *ChatModel) makeMetaToolExecutor(inner llm.ToolExecutor) llm.ToolExecutor {
	ep := m.endpoint
	modelTag := m.modelTag
	provider := m.searchProvider
	apiKey := m.searchAPIKey
	contextLimit := m.contextLimit
	mode := m.permissionMode
	ch := m.confirmCh

	return func(ctx context.Context, name, argsJSON string) (string, bool) {
		switch name {
		case "web_search":
			return executeWebSearch(ctx, provider, apiKey, contextLimit, argsJSON)
		case "deep_research":
			return executeDeepResearch(ctx, provider, apiKey, ep, modelTag, contextLimit, argsJSON)
		case "fetch_page":
			return executeFetchPage(ctx, contextLimit, argsJSON)
		case "remember":
			return executeRemember(ctx, argsJSON)
		case "review_code":
			return executeReviewCode(ctx, contextLimit)
		case "commit_changes":
			return executeDestructive(ctx, mode, ch, name, argsJSON, func() (string, bool) {
				return executeCommitChanges(ctx, argsJSON)
			})
		case "create_pr":
			return executeDestructive(ctx, mode, ch, name, argsJSON, func() (string, bool) {
				return executeCreatePR(ctx, argsJSON)
			})
		case "run_tests":
			return executeDestructive(ctx, mode, ch, name, argsJSON, func() (string, bool) {
				return executeRunTests(ctx, argsJSON)
			})
		case "review_pr":
			return executeReviewPR(ctx, contextLimit, argsJSON)
		case "read_issue":
			return executeReadIssue(ctx, contextLimit, argsJSON)
		case "pr_status":
			return executePRStatus(ctx, argsJSON)
		case "create_branch":
			return executeDestructive(ctx, mode, ch, name, argsJSON, func() (string, bool) {
				return executeCreateBranch(ctx, argsJSON)
			})
		default:
			return inner(ctx, name, argsJSON)
		}
	}
}

// executeDestructive gates a destructive meta-tool behind the permission system.
// In "suggest" mode it blocks; in "confirm" mode it asks the user; in "auto" it proceeds.
func executeDestructive(ctx context.Context, mode string, ch chan confirmRequest, name, argsJSON string, fn func() (string, bool)) (string, bool) {
	switch mode {
	case "suggest":
		return fmt.Sprintf("[suggest mode] Would run %s — approve with --yolo or permission_mode: auto", name), true
	case "confirm":
		prompt := formatConfirmPrompt(name, argsJSON)
		respCh := make(chan bool, 1)
		ch <- confirmRequest{
			Name:   name,
			Args:   argsJSON,
			Prompt: prompt,
			RespCh: respCh,
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
	return fn()
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

// executeFetchPage fetches a URL and returns extracted text content.
func executeFetchPage(ctx context.Context, contextLimit int, argsJSON string) (string, bool) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), true
	}
	if args.URL == "" {
		return "missing required parameter: url", true
	}

	logger.Debug("meta-tool fetch_page", "url", args.URL)

	content, err := search.FetchContent(args.URL)
	if err != nil {
		return fmt.Sprintf("fetch error: %v", err), true
	}

	budget := contextLimit // ~25% of context window in chars
	return search.TruncateContent(content, budget), false
}

// executeRemember saves a fact to persistent memory.
func executeRemember(ctx context.Context, argsJSON string) (string, bool) {
	var args struct {
		Fact string `json:"fact"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), true
	}
	if args.Fact == "" {
		return "missing required parameter: fact", true
	}

	logger.Debug("meta-tool remember", "fact", args.Fact)

	// Default to project scope if .baryo/ dir exists, otherwise global.
	global := true
	if _, err := os.Stat(".baryo"); err == nil {
		global = false
	}

	if err := config.AddMemory(args.Fact, global); err != nil {
		return fmt.Sprintf("failed to save memory: %v", err), true
	}

	scope := "global"
	if !global {
		scope = "project"
	}
	return fmt.Sprintf("Remembered (%s): %s", scope, args.Fact), false
}

// executeReviewCode returns the current git diff for review.
func executeReviewCode(ctx context.Context, contextLimit int) (string, bool) {
	logger.Debug("meta-tool review_code")

	// Try staged changes first, then all changes.
	diff, err := gitOutput("diff", "--cached")
	if err != nil || diff == "" {
		diff, err = gitOutput("diff")
	}
	if err != nil {
		return fmt.Sprintf("git error: %v", err), true
	}
	if diff == "" {
		return "No changes detected. Working tree is clean.", false
	}

	budget := contextLimit * 2 // generous budget for diffs
	return search.TruncateContent(diff, budget), false
}

// executeCommitChanges stages all changes and commits with a message.
func executeCommitChanges(ctx context.Context, argsJSON string) (string, bool) {
	var args struct {
		Message string `json:"message"`
	}
	if argsJSON != "" {
		json.Unmarshal([]byte(argsJSON), &args)
	}

	logger.Debug("meta-tool commit_changes", "message", args.Message)

	// Stage all changes.
	if _, err := gitOutput("add", "-A"); err != nil {
		return fmt.Sprintf("git add failed: %v", err), true
	}

	// Check if there's anything to commit.
	status, _ := gitOutput("status", "--porcelain")
	if status == "" {
		return "Nothing to commit. Working tree is clean.", false
	}

	msg := args.Message
	if msg == "" {
		// Auto-generate from diff.
		diff, _ := gitOutput("diff", "--cached", "--stat")
		if diff == "" {
			diff = "misc changes"
		}
		// Use a simple default; the model can provide better messages.
		msg = "chore: update files"
		// Try to infer from diff stat.
		lines := strings.Split(diff, "\n")
		if len(lines) > 0 {
			last := strings.TrimSpace(lines[len(lines)-1])
			if last != "" {
				msg = fmt.Sprintf("chore: %s", last)
			}
		}
	}

	if _, err := gitOutput("commit", "-m", msg); err != nil {
		return fmt.Sprintf("git commit failed: %v", err), true
	}

	return fmt.Sprintf("Committed: %s", msg), false
}

// executeCreatePR pushes the current branch and creates a GitHub PR.
func executeCreatePR(ctx context.Context, argsJSON string) (string, bool) {
	var args struct {
		Title string `json:"title"`
	}
	if argsJSON != "" {
		json.Unmarshal([]byte(argsJSON), &args)
	}

	logger.Debug("meta-tool create_pr", "title", args.Title)

	// Check that gh CLI is available.
	if _, err := exec.LookPath("gh"); err != nil {
		return "gh CLI not found. Install it from https://cli.github.com/", true
	}

	// Get current branch.
	branch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Sprintf("git error: %v", err), true
	}
	if branch == "main" || branch == "master" {
		return "Cannot create PR from main/master branch. Create a feature branch first.", true
	}

	// Push branch.
	cmd := exec.CommandContext(ctx, "git", "push", "-u", "origin", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("git push failed: %s\n%s", err, strings.TrimSpace(string(out))), true
	}

	// Create PR.
	prArgs := []string{"pr", "create", "--fill"}
	if args.Title != "" {
		prArgs = []string{"pr", "create", "--title", args.Title, "--fill"}
	}
	cmd = exec.CommandContext(ctx, "gh", prArgs...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Sprintf("gh pr create failed: %s\n%s", err, output), true
	}

	return fmt.Sprintf("PR created: %s", output), false
}

// executeRunTests auto-detects the test framework and runs tests.
func executeRunTests(ctx context.Context, argsJSON string) (string, bool) {
	var args struct {
		Path string `json:"path"`
	}
	if argsJSON != "" {
		json.Unmarshal([]byte(argsJSON), &args)
	}

	logger.Debug("meta-tool run_tests", "path", args.Path)

	runner, runArgs := detectTestRunner(args.Path)
	if runner == "" {
		return "Could not detect test framework. No go.mod, package.json, Cargo.toml, or pyproject.toml found.", true
	}

	cmd := exec.CommandContext(ctx, runner, runArgs...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	budget := 8000 // cap test output
	output = search.TruncateContent(output, budget)

	if err != nil {
		return fmt.Sprintf("Tests failed:\n%s", output), true
	}
	return fmt.Sprintf("Tests passed:\n%s", output), false
}

// detectTestRunner checks for common project files and returns the test command.
func detectTestRunner(path string) (string, []string) {
	if _, err := os.Stat("go.mod"); err == nil {
		target := "./..."
		if path != "" {
			target = path
		}
		return "go", []string{"test", target}
	}
	if _, err := os.Stat("package.json"); err == nil {
		if path != "" {
			return "npx", []string{"jest", path}
		}
		return "npm", []string{"test"}
	}
	if _, err := os.Stat("Cargo.toml"); err == nil {
		args := []string{"test"}
		if path != "" {
			args = append(args, "-p", path)
		}
		return "cargo", args
	}
	if _, err := os.Stat("pyproject.toml"); err == nil {
		args := []string{"-m", "pytest"}
		if path != "" {
			args = append(args, path)
		}
		return "python", args
	}
	if _, err := os.Stat("setup.py"); err == nil {
		args := []string{"-m", "pytest"}
		if path != "" {
			args = append(args, path)
		}
		return "python", args
	}
	return "", nil
}

// executeReviewPR fetches a PR's metadata, comments, and diff for model analysis.
func executeReviewPR(ctx context.Context, contextLimit int, argsJSON string) (string, bool) {
	var args struct {
		Number string `json:"number"`
	}
	if argsJSON != "" {
		json.Unmarshal([]byte(argsJSON), &args)
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return "gh CLI not found. Install it from https://cli.github.com/", true
	}

	logger.Debug("meta-tool review_pr", "number", args.Number)

	// Determine which PR to review.
	prRef := args.Number
	if prRef == "" {
		// Get the latest PR number.
		out, err := ghOutput(ctx, "pr", "list", "--limit", "1", "--json", "number", "--jq", ".[0].number")
		if err != nil || strings.TrimSpace(out) == "" {
			return "No open PRs found.", true
		}
		prRef = strings.TrimSpace(out)
	}

	// Fetch PR metadata + comments.
	meta, err := ghOutput(ctx, "pr", "view", prRef, "--json", "title,body,state,reviewDecision,reviews,comments")
	if err != nil {
		return fmt.Sprintf("gh pr view failed: %v", err), true
	}

	// Fetch PR diff.
	diff, err := ghOutput(ctx, "pr", "diff", prRef)
	if err != nil {
		return fmt.Sprintf("gh pr diff failed: %v", err), true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## PR #%s Metadata\n%s\n\n## Diff\n%s", prRef, meta, diff)

	budget := contextLimit * 2
	return search.TruncateContent(b.String(), budget), false
}

// executeReadIssue fetches a GitHub issue's body and comments.
func executeReadIssue(ctx context.Context, contextLimit int, argsJSON string) (string, bool) {
	var args struct {
		Number string `json:"number"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), true
	}
	if args.Number == "" {
		return "missing required parameter: number", true
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return "gh CLI not found. Install it from https://cli.github.com/", true
	}

	logger.Debug("meta-tool read_issue", "number", args.Number)

	out, err := ghOutput(ctx, "issue", "view", args.Number, "--json", "title,body,state,labels,comments")
	if err != nil {
		return fmt.Sprintf("gh issue view failed: %v", err), true
	}

	budget := contextLimit * 2
	return search.TruncateContent(out, budget), false
}

// executePRStatus shows the PR review status for the current repository.
func executePRStatus(ctx context.Context, argsJSON string) (string, bool) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "gh CLI not found. Install it from https://cli.github.com/", true
	}

	logger.Debug("meta-tool pr_status")

	out, err := ghOutput(ctx, "pr", "status")
	if err != nil {
		return fmt.Sprintf("gh pr status failed: %v", err), true
	}

	return out, false
}

// executeCreateBranch creates and checks out a new git branch.
func executeCreateBranch(ctx context.Context, argsJSON string) (string, bool) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("invalid arguments: %v", err), true
	}
	if args.Name == "" {
		return "missing required parameter: name", true
	}

	logger.Debug("meta-tool create_branch", "name", args.Name)

	// Validate branch name: no spaces, not main/master.
	name := strings.TrimSpace(args.Name)
	if strings.ContainsAny(name, " \t") {
		return "branch name cannot contain spaces", true
	}
	if name == "main" || name == "master" {
		return "cannot create a branch named main or master", true
	}

	if _, err := gitOutput("checkout", "-b", name); err != nil {
		return fmt.Sprintf("git checkout -b failed: %v", err), true
	}

	return fmt.Sprintf("Created and switched to branch: %s", name), false
}

// ghOutput runs a gh CLI command and returns its trimmed output.
func ghOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
