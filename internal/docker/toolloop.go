// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
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
func StreamChatWithTools(ctx context.Context, socketPath, model string, messages []ChatMessage, params ChatParams, toolDefs []ToolDefinition, executor ToolExecutor) <-chan StreamEvent {
	out := make(chan StreamEvent, 64)

	go func() {
		defer close(out)

		msgs := make([]ChatMessage, len(messages))
		copy(msgs, messages)

		for round := 0; round < maxToolRounds; round++ {
			evtCh, resCh := streamChatRaw(ctx, socketPath, model, msgs, params, toolDefs)

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
				out <- StreamEvent{Done: true}
				return
			}

			// Get the accumulated result.
			res, ok := <-resCh
			if !ok {
				out <- StreamEvent{Done: true}
				return
			}

			// No structured tool calls — try text-based fallback.
			if len(res.ToolCalls) == 0 {
				validNames := buildValidToolNames(toolDefs)
				textCalls := parseTextToolCalls(contentBuf, validNames)
				if len(textCalls) == 0 {
					out <- StreamEvent{Done: true}
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

			// Sanitize tool call IDs to meet model template requirements.
			for i := range res.ToolCalls {
				res.ToolCalls[i].ID = sanitizeToolCallID(res.ToolCalls[i].ID)
			}

			// Append the assistant message with tool calls.
			assistantMsg := NewChatMessage("assistant", contentBuf)
			assistantMsg.ToolCalls = res.ToolCalls
			msgs = append(msgs, assistantMsg)

			// Execute each tool call and append results.
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

				// Build the tool result message for the conversation.
				resultContent := content
				if isError {
					errResp := map[string]string{"error": content}
					if b, err := json.Marshal(errResp); err == nil {
						resultContent = string(b)
					}
				}
				toolMsg := NewChatMessage("tool", resultContent)
				toolMsg.ToolCallID = tc.ID
				toolMsg.Name = tc.Function.Name
				msgs = append(msgs, toolMsg)
			}
			// Loop back for the next round.
		}

		// Max rounds exceeded — emit done.
		out <- StreamEvent{Done: true}
	}()

	return out
}
