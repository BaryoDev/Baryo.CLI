// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// --- Anthropic request types ---

type anthropicRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	Messages  []anthropicMessage  `json:"messages"`
	System    string              `json:"system,omitempty"`
	Tools     []anthropicTool     `json:"tools,omitempty"`
	Stream    bool                `json:"stream"`
	Thinking  *anthropicThinking  `json:"thinking,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessage struct {
	Role    string                 `json:"role"`
	Content interface{}            `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`                 // "text", "tool_use", "tool_result"
	Text      string          `json:"text,omitempty"`       // for type=text
	ID        string          `json:"id,omitempty"`         // for type=tool_use
	Name      string          `json:"name,omitempty"`       // for type=tool_use
	Input     json.RawMessage `json:"input,omitempty"`      // for type=tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // for type=tool_result
	Content   string          `json:"content,omitempty"`     // for type=tool_result (reused)
}

type anthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

// --- Anthropic SSE response types ---

type anthropicSSEMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Role  string `json:"role"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type anthropicSSEContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type  string          `json:"type"` // "text" or "tool_use"
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Text  string          `json:"text,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content_block"`
}

type anthropicSSEContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"` // "text_delta" or "input_json_delta"
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

type anthropicSSEMessageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// isThinkingCapable returns true if the model supports Anthropic's extended thinking API.
func isThinkingCapable(model string) bool {
	thinkingModels := []string{
		"claude-3-5-sonnet",
		"claude-3-7-sonnet",
		"claude-sonnet-4",
		"claude-opus-4",
	}
	for _, prefix := range thinkingModels {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

// --- Conversion helpers ---

// convertToAnthropicMessages extracts the system prompt (if any) and converts
// ChatMessages to Anthropic's message format.
func convertToAnthropicMessages(msgs []ChatMessage) (string, []anthropicMessage) {
	var system string
	var out []anthropicMessage

	for _, m := range msgs {
		switch m.Role {
		case "system":
			if m.Content != nil {
				system = *m.Content
			}
		case "user":
			if m.Content != nil {
				out = append(out, anthropicMessage{Role: "user", Content: *m.Content})
			}
		case "assistant":
			if len(m.ToolCalls) > 0 {
				// Assistant message with tool calls → content blocks.
				var blocks []anthropicContentBlock
				if m.Content != nil && *m.Content != "" {
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: *m.Content})
				}
				for _, tc := range m.ToolCalls {
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: json.RawMessage(tc.Function.Arguments),
					})
				}
				out = append(out, anthropicMessage{Role: "assistant", Content: blocks})
			} else if m.Content != nil {
				out = append(out, anthropicMessage{Role: "assistant", Content: *m.Content})
			}
		case "tool":
			// Tool results go as user messages with tool_result content blocks.
			block := anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   "",
			}
			if m.Content != nil {
				block.Content = *m.Content
			}
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicContentBlock{block}})
		}
	}

	return system, out
}

// convertToAnthropicTools converts ToolDefinitions to Anthropic's tool format.
func convertToAnthropicTools(tools []ToolDefinition) []anthropicTool {
	out := make([]anthropicTool, len(tools))
	for i, t := range tools {
		out[i] = anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		}
	}
	return out
}

// --- Streaming ---

// streamChatAnthropic sends a chat request to the Anthropic Messages API
// and streams events. Same return signature as streamChatRaw.
func streamChatAnthropic(ctx context.Context, ep Endpoint, model string, messages []ChatMessage, params ChatParams, tools []ToolDefinition) (<-chan StreamEvent, <-chan streamResult) {
	ch := make(chan StreamEvent, 64)
	resCh := make(chan streamResult, 1)

	go func() {
		defer close(ch)
		defer close(resCh)

		system, anthMsgs := convertToAnthropicMessages(messages)

		maxTokens := 8192
		if params.MaxTokens != nil {
			maxTokens = *params.MaxTokens
		}

		reqBody := anthropicRequest{
			Model:     model,
			MaxTokens: maxTokens,
			Messages:  anthMsgs,
			System:    system,
			Stream:    true,
		}
		if len(tools) > 0 {
			reqBody.Tools = convertToAnthropicTools(tools)
		}

		// Enable extended thinking for capable models.
		if isThinkingCapable(model) {
			budgetTokens := maxTokens / 2
			if budgetTokens < 1024 {
				budgetTokens = 1024
			}
			reqBody.Thinking = &anthropicThinking{
				Type:         "enabled",
				BudgetTokens: budgetTokens,
			}
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("%v", err)}
			return
		}

		url := ep.BaseURL + "/messages"

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("%v", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", ep.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		client := newHTTPClient(ep)
		resp, err := client.Do(req)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("%v", err)}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			detail := strings.TrimSpace(string(errBody))
			if detail != "" {
				ch <- StreamEvent{Error: fmt.Sprintf("anthropic returned %d: %s", resp.StatusCode, detail)}
			} else {
				ch <- StreamEvent{Error: fmt.Sprintf("anthropic returned %d", resp.StatusCode)}
			}
			return
		}

		// Track tool calls by content block index.
		type toolCallAcc struct {
			id       string
			funcName string
			args     strings.Builder
		}
		toolCalls := make(map[int]*toolCallAcc)

		// Track thinking block indices.
		thinkingBlocks := make(map[int]bool)

		var inputTokens, outputTokens int
		var finishReason string

		scanner := bufio.NewScanner(resp.Body)
		// Anthropic can return large tool call arguments; increase buffer.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var eventType string
		for scanner.Scan() {
			line := scanner.Text()

			// Parse SSE event type.
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			switch eventType {
			case "message_start":
				var msg anthropicSSEMessageStart
				if err := json.Unmarshal([]byte(data), &msg); err == nil {
					inputTokens = msg.Message.Usage.InputTokens
					outputTokens = msg.Message.Usage.OutputTokens
				}

			case "content_block_start":
				var block anthropicSSEContentBlockStart
				if err := json.Unmarshal([]byte(data), &block); err == nil {
					switch block.ContentBlock.Type {
					case "tool_use":
						toolCalls[block.Index] = &toolCallAcc{
							id:       block.ContentBlock.ID,
							funcName: block.ContentBlock.Name,
						}
					case "thinking":
						thinkingBlocks[block.Index] = true
					}
				}

			case "content_block_delta":
				var delta anthropicSSEContentBlockDelta
				if err := json.Unmarshal([]byte(data), &delta); err == nil {
					switch delta.Delta.Type {
					case "text_delta":
						select {
						case ch <- StreamEvent{Token: delta.Delta.Text}:
						case <-ctx.Done():
							return
						}
					case "thinking_delta":
						if thinkingBlocks[delta.Index] {
							select {
							case ch <- StreamEvent{ThinkingToken: delta.Delta.Text}:
							case <-ctx.Done():
								return
							}
						}
					case "input_json_delta":
						if tc, ok := toolCalls[delta.Index]; ok {
							tc.args.WriteString(delta.Delta.PartialJSON)
						}
					}
				}

			case "message_delta":
				var msgDelta anthropicSSEMessageDelta
				if err := json.Unmarshal([]byte(data), &msgDelta); err == nil {
					finishReason = msgDelta.Delta.StopReason
					outputTokens = msgDelta.Usage.OutputTokens
				}

			case "message_stop":
				// Stream complete.

			case "error":
				// Anthropic error event.
				var errEvt struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(data), &errEvt); err == nil && errEvt.Error.Message != "" {
					ch <- StreamEvent{Error: errEvt.Error.Message}
					return
				}
			}
		}

		// Build completed tool calls.
		var calls []ToolCall
		for _, acc := range toolCalls {
			calls = append(calls, ToolCall{
				ID:   acc.id,
				Type: "function",
				Function: FunctionCall{
					Name:      acc.funcName,
					Arguments: acc.args.String(),
				},
			})
		}

		// Map Anthropic stop reasons to OpenAI-compatible values.
		switch finishReason {
		case "end_turn":
			finishReason = "stop"
		case "tool_use":
			finishReason = "tool_calls"
		case "max_tokens":
			finishReason = "length"
		}

		var usage *UsageStats
		if inputTokens > 0 || outputTokens > 0 {
			usage = &UsageStats{
				PromptTokens:     inputTokens,
				CompletionTokens: outputTokens,
				TotalTokens:      inputTokens + outputTokens,
			}
		}

		resCh <- streamResult{ToolCalls: calls, Usage: usage, FinishReason: finishReason}
	}()

	return ch, resCh
}
