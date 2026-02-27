// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/tools"
)

// PrintOptions holds all configuration for headless print mode.
type PrintOptions struct {
	Endpoint       docker.Endpoint
	SystemPrompt   string
	Model          docker.DockerModel
	Prompt         string
	Params         docker.ChatParams
	PermissionMode string
	MaxTurns       int
	OutputFormat   string // "text" or "json"
	EnableTools    bool
}

// RunPrint runs a single prompt through the model in headless mode.
// Returns an exit code: 0 for success, 1 for runtime errors.
func RunPrint(opts PrintOptions) int {
	if opts.OutputFormat == "json" {
		return runPrintJSON(opts)
	}
	return runPrintText(opts)
}

// runPrintText streams tokens to stdout and tool status to stderr.
func runPrintText(opts PrintOptions) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	messages := buildMessages(opts)

	if !opts.EnableTools {
		return streamSimple(ctx, opts, messages)
	}

	toolDefs := tools.DockerDefinitions()
	executor := makeHeadlessExecutor(opts.PermissionMode)
	maxRounds := opts.MaxTurns
	if maxRounds <= 0 {
		maxRounds = 5
	}

	ch := docker.StreamChatWithToolsN(ctx, opts.Endpoint, opts.Model.Tag, messages, opts.Params, toolDefs, executor, maxRounds)

	for evt := range ch {
		if evt.Error != "" {
			fmt.Fprintf(os.Stderr, "error: %s\n", evt.Error)
			return 1
		}
		if evt.Token != "" {
			fmt.Print(evt.Token)
		}
		if evt.ToolStart != nil {
			fmt.Fprintf(os.Stderr, "[tool] %s\n", evt.ToolStart.Name)
		}
		if evt.ToolResult != nil {
			status := "ok"
			if evt.ToolResult.IsError {
				status = "error"
			}
			fmt.Fprintf(os.Stderr, "[tool] %s: %s\n", evt.ToolResult.Name, status)
		}
	}

	fmt.Println()
	return 0
}

// jsonOutput is the structured output for JSON mode.
type jsonOutput struct {
	Content   string         `json:"content"`
	ToolCalls []jsonToolCall `json:"tool_calls,omitempty"`
	Usage     *jsonUsage     `json:"usage,omitempty"`
	ExitCode  int            `json:"exit_code"`
}

type jsonToolCall struct {
	Name    string `json:"name"`
	Args    string `json:"args"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

type jsonUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// runPrintJSON collects all events and prints a single JSON object to stdout.
func runPrintJSON(opts PrintOptions) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	messages := buildMessages(opts)

	out := jsonOutput{}

	if !opts.EnableTools {
		ch := docker.StreamChat(ctx, opts.Endpoint, opts.Model.Tag, messages, opts.Params)
		for evt := range ch {
			if evt.Error != "" {
				out.ExitCode = 1
				out.Content = evt.Error
				printJSON(out)
				return 1
			}
			if evt.Token != "" {
				out.Content += evt.Token
			}
			if evt.Usage != nil {
				out.Usage = &jsonUsage{
					PromptTokens:     evt.Usage.PromptTokens,
					CompletionTokens: evt.Usage.CompletionTokens,
					TotalTokens:      evt.Usage.TotalTokens,
				}
			}
		}
		printJSON(out)
		return 0
	}

	toolDefs := tools.DockerDefinitions()
	executor := makeHeadlessExecutor(opts.PermissionMode)
	maxRounds := opts.MaxTurns
	if maxRounds <= 0 {
		maxRounds = 5
	}

	ch := docker.StreamChatWithToolsN(ctx, opts.Endpoint, opts.Model.Tag, messages, opts.Params, toolDefs, executor, maxRounds)

	// Track the current tool call being built (for pairing start → result).
	var pendingTool *jsonToolCall

	for evt := range ch {
		if evt.Error != "" {
			out.ExitCode = 1
			out.Content = evt.Error
			printJSON(out)
			return 1
		}
		if evt.ContentReplace != nil {
			out.Content = *evt.ContentReplace
		} else if evt.Token != "" {
			out.Content += evt.Token
		}
		if evt.ToolStart != nil {
			pendingTool = &jsonToolCall{
				Name: evt.ToolStart.Name,
				Args: evt.ToolStart.Args,
			}
		}
		if evt.ToolResult != nil {
			if pendingTool != nil {
				pendingTool.Result = evt.ToolResult.Content
				pendingTool.IsError = evt.ToolResult.IsError
				out.ToolCalls = append(out.ToolCalls, *pendingTool)
				pendingTool = nil
			} else {
				out.ToolCalls = append(out.ToolCalls, jsonToolCall{
					Name:    evt.ToolResult.Name,
					Result:  evt.ToolResult.Content,
					IsError: evt.ToolResult.IsError,
				})
			}
		}
		if evt.Usage != nil {
			out.Usage = &jsonUsage{
				PromptTokens:     evt.Usage.PromptTokens,
				CompletionTokens: evt.Usage.CompletionTokens,
				TotalTokens:      evt.Usage.TotalTokens,
			}
		}
	}

	printJSON(out)
	return 0
}

// buildMessages constructs the initial message list for print mode.
func buildMessages(opts PrintOptions) []docker.ChatMessage {
	var messages []docker.ChatMessage
	if opts.SystemPrompt != "" {
		messages = append(messages, docker.NewChatMessage("system", opts.SystemPrompt))
	}
	messages = append(messages, docker.NewChatMessage("user", opts.Prompt))
	return messages
}

// streamSimple streams a simple chat without tools (backward compatible path).
func streamSimple(ctx context.Context, opts PrintOptions, messages []docker.ChatMessage) int {
	ch := docker.StreamChat(ctx, opts.Endpoint, opts.Model.Tag, messages, opts.Params)
	for evt := range ch {
		if evt.Error != "" {
			fmt.Fprintf(os.Stderr, "error: %s\n", evt.Error)
			return 1
		}
		if evt.Token != "" {
			fmt.Print(evt.Token)
		}
	}
	fmt.Println()
	return 0
}

// makeHeadlessExecutor returns a tool executor for headless mode.
// In "auto" mode, all tools are executed. In other modes, destructive tools
// are blocked with an error message.
func makeHeadlessExecutor(permissionMode string) docker.ToolExecutor {
	return func(ctx context.Context, name, argsJSON string) (string, bool) {
		if permissionMode != "auto" && tools.IsDestructive(name) {
			return fmt.Sprintf("tool %q requires --yolo flag for headless execution", name), true
		}
		result := tools.Execute(ctx, name, argsJSON)
		return result.Content, result.IsError
	}
}

// printJSON marshals v as indented JSON and writes it to stdout.
func printJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to marshal JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}
