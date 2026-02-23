// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// newHTTPClient creates an HTTP client that connects via unix socket or TCP.
// TCP paths are detected by "tcp://" prefix or "host:port" format.
func newHTTPClient(socketPath string) *http.Client {
	dial := func(_ context.Context, _, _ string) (net.Conn, error) {
		network, addr := parseSocketAddr(socketPath)
		return net.DialTimeout(network, addr, 30*time.Second)
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dial,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// parseSocketAddr determines the network type and address from a socket path.
func parseSocketAddr(socketPath string) (network, addr string) {
	if strings.HasPrefix(socketPath, "tcp://") {
		return "tcp", strings.TrimPrefix(socketPath, "tcp://")
	}
	// Bare host:port (e.g. "localhost:12434")
	if host, port, err := net.SplitHostPort(socketPath); err == nil && host != "" && port != "" {
		return "tcp", socketPath
	}
	return "unix", socketPath
}

// streamResult holds the final state after a raw streaming call completes.
type streamResult struct {
	ToolCalls []ToolCall // accumulated tool calls (if any)
}

// streamChatRaw sends a chat request and streams events into the returned channel.
// The channel is closed when streaming ends. On completion the streamResult is
// sent via the result channel so callers can inspect accumulated tool calls.
func streamChatRaw(ctx context.Context, socketPath, model string, messages []ChatMessage, params ChatParams, tools []ToolDefinition) (<-chan StreamEvent, <-chan streamResult) {
	ch := make(chan StreamEvent, 64)
	resCh := make(chan streamResult, 1)

	go func() {
		defer close(ch)
		defer close(resCh)

		reqBody := ChatRequest{
			Model:       model,
			Messages:    messages,
			Stream:      true,
			Temperature: params.Temperature,
			TopP:        params.TopP,
			MaxTokens:   params.MaxTokens,
			Tools:       tools,
		}
		if len(tools) > 0 {
			reqBody.ToolChoice = "auto"
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("%v", err)}
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST",
			"http://localhost/v1/chat/completions",
			bytes.NewReader(body))
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("%v", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := newHTTPClient(socketPath)
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
				ch <- StreamEvent{Error: fmt.Sprintf("server returned %d: %s", resp.StatusCode, detail)}
			} else {
				ch <- StreamEvent{Error: fmt.Sprintf("server returned %d", resp.StatusCode)}
			}
			return
		}

		// Accumulate tool call fragments by index.
		type toolCallAcc struct {
			id       string
			funcName string
			args     strings.Builder
		}
		var toolAccs []toolCallAcc

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				break
			}

			var chunk StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			for _, choice := range chunk.Choices {
				// Text content
				if choice.Delta.Content != "" {
					select {
					case ch <- StreamEvent{Token: choice.Delta.Content}:
					case <-ctx.Done():
						return
					}
				}

				// Tool call fragments
				for _, dtc := range choice.Delta.ToolCalls {
					// Grow accumulator slice if needed
					for dtc.Index >= len(toolAccs) {
						toolAccs = append(toolAccs, toolCallAcc{})
					}
					if dtc.ID != "" {
						toolAccs[dtc.Index].id = dtc.ID
					}
					if dtc.Function != nil {
						if dtc.Function.Name != "" {
							toolAccs[dtc.Index].funcName = dtc.Function.Name
						}
						toolAccs[dtc.Index].args.WriteString(dtc.Function.Arguments)
					}
				}
			}
		}

		// Build completed tool calls.
		var calls []ToolCall
		for _, acc := range toolAccs {
			calls = append(calls, ToolCall{
				ID:   acc.id,
				Type: "function",
				Function: FunctionCall{
					Name:      acc.funcName,
					Arguments: acc.args.String(),
				},
			})
		}

		resCh <- streamResult{ToolCalls: calls}
	}()

	return ch, resCh
}

// StreamChat sends a chat request and streams events. No tools are provided.
func StreamChat(ctx context.Context, socketPath, model string, messages []ChatMessage, params ChatParams) <-chan StreamEvent {
	ch, _ := streamChatRaw(ctx, socketPath, model, messages, params, nil)
	out := make(chan StreamEvent, 64)
	go func() {
		defer close(out)
		for evt := range ch {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
		out <- StreamEvent{Done: true}
	}()
	return out
}
