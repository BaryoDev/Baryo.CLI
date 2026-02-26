// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const maxToolRounds = 5

// ToolExecutor is a function that executes a tool by name with JSON arguments.
// It returns the result content and whether the result is an error.
type ToolExecutor func(ctx context.Context, name, argsJSON string) (content string, isError bool)

// validToolCallID matches exactly 9 alphanumeric characters.
var validToolCallID = regexp.MustCompile(`^[a-zA-Z0-9]{9}$`)

// sanitizeToolCallID ensures the ID is a 9-character alphanumeric string.
// If the model returns a non-compliant ID, generate a new one.
func sanitizeToolCallID(id string) string {
	if validToolCallID.MatchString(id) {
		return id
	}
	b := make([]byte, 5)
	rand.Read(b)
	return hex.EncodeToString(b)[:9]
}

// StreamChatWithTools streams a conversation that may include tool calls.
// The model can call tools up to maxToolRounds times before the final response.
func StreamChatWithTools(ctx context.Context, ep Endpoint, model string, messages []ChatMessage, params ChatParams, toolDefs []ToolDefinition, executor ToolExecutor) <-chan StreamEvent {
	out := make(chan StreamEvent, 64)

	go func() {
		defer close(out)

		msgs := make([]ChatMessage, len(messages))
		copy(msgs, messages)

		var lastUsage *UsageStats

		for round := 0; round < maxToolRounds; round++ {
			// Only pass tool definitions on the first round. Continuation
			// rounds use user-role results which don't require the "tool" role
			// that many local models (Gemma, etc.) reject.
			var roundTools []ToolDefinition
			if round == 0 {
				roundTools = toolDefs
			}
			evtCh, resCh := streamChatRaw(ctx, ep, model, msgs, params, roundTools)

			// Forward all streaming events (tokens, errors).
			var contentBuf string
			var hadError bool
			for evt := range evtCh {
				if evt.Error != "" {
					hadError = true
				}
				if evt.Token != "" {
					contentBuf += evt.Token
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			}

			if hadError {
				out <- StreamEvent{Done: true, Usage: lastUsage}
				return
			}

			// Get the accumulated result.
			res, ok := <-resCh
			if !ok {
				out <- StreamEvent{Done: true, Usage: lastUsage}
				return
			}

			// Track usage from each round.
			if res.Usage != nil {
				lastUsage = res.Usage
			}

			// Check for text-based tool calls if no native ones were returned.
			if len(res.ToolCalls) == 0 {
				validNames := buildValidToolNames(toolDefs)
				textCalls := parseTextToolCalls(contentBuf, validNames)
				if len(textCalls) == 0 {
					out <- StreamEvent{Done: true, Usage: lastUsage}
					return
				}

				// Strip tool-call text from the displayed content.
				cleanContent := stripToolCallText(contentBuf, textCalls)
				select {
				case out <- StreamEvent{ContentReplace: &cleanContent}:
				case <-ctx.Done():
					return
				}
				contentBuf = cleanContent

				// Convert text calls into synthetic ToolCalls.
				for _, tc := range textCalls {
					res.ToolCalls = append(res.ToolCalls, ToolCall{
						ID:   sanitizeToolCallID(""),
						Type: "function",
						Function: FunctionCall{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					})
				}
			}

			// Execute each tool call and collect results.
			type toolResult struct {
				tc      ToolCall
				content string
				isError bool
			}
			var results []toolResult

			for _, tc := range res.ToolCalls {
				select {
				case out <- StreamEvent{ToolStart: &ToolStartEvent{
					CallID: tc.ID,
					Name:   tc.Function.Name,
					Args:   tc.Function.Arguments,
				}}:
				case <-ctx.Done():
					return
				}

				content, isError := executor(ctx, tc.Function.Name, tc.Function.Arguments)

				select {
				case out <- StreamEvent{ToolResult: &ToolResultEvent{
					CallID:  tc.ID,
					Name:    tc.Function.Name,
					Content: content,
					IsError: isError,
				}}:
				case <-ctx.Done():
					return
				}

				results = append(results, toolResult{tc: tc, content: content, isError: isError})
			}

			// Always use user-role for tool results to maintain role alternation.
			// Many local models (Gemma, Mistral) don't support the "tool" role
			// in their chat templates.
			msgs = append(msgs, NewChatMessage("assistant", contentBuf))

			var resultBuf strings.Builder
			for _, r := range results {
				status := "OK"
				if r.isError {
					status = "ERROR"
				}
				fmt.Fprintf(&resultBuf, "[%s result (%s)]\n%s\n\n", r.tc.Function.Name, status, r.content)
			}
			msgs = append(msgs, NewChatMessage("user", strings.TrimSpace(resultBuf.String())))
			// Loop back for the next round.
		}

		// Max rounds exceeded — emit done.
		out <- StreamEvent{Done: true, Usage: lastUsage}
	}()

	return out
}
