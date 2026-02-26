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

// newHTTPClient creates an HTTP client for the given endpoint.
// Remote HTTPS providers use a standard client; local/TCP use socket dialing.
func newHTTPClient(ep Endpoint) *http.Client {
	if ep.IsRemote() {
		return &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 2 * time.Minute,
			},
		}
	}

	network, addr := parseSocketAddr(ep.SocketPath)
	dial := func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.DialTimeout(network, addr, 30*time.Second)
	}

	// Remote Ollama servers may need minutes to load large models on first request.
	headerTimeout := 120 * time.Second
	if network == "tcp" {
		headerTimeout = 10 * time.Minute
	}

	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dial,
			ResponseHeaderTimeout: headerTimeout,
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
	ToolCalls []ToolCall  // accumulated tool calls (if any)
	Usage     *UsageStats // token usage from the final SSE frame (if reported)
}

// streamChatRaw sends a chat request and streams events into the returned channel.
// The channel is closed when streaming ends. On completion the streamResult is
// sent via the result channel so callers can inspect accumulated tool calls.
func streamChatRaw(ctx context.Context, ep Endpoint, model string, messages []ChatMessage, params ChatParams, tools []ToolDefinition) (<-chan StreamEvent, <-chan streamResult) {
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
			TopK:        params.TopK,
			Stop:        params.Stop,
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

		url := "http://localhost/v1/chat/completions"
		if ep.IsRemote() {
			url = ep.BaseURL + "/chat/completions"
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			ch <- StreamEvent{Error: fmt.Sprintf("%v", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if ep.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+ep.APIKey)
		}

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
		var lastUsage *UsageStats

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

			// Capture usage stats if present (typically in the final frame).
			if chunk.Usage != nil {
				lastUsage = chunk.Usage
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

		resCh <- streamResult{ToolCalls: calls, Usage: lastUsage}
	}()

	return ch, resCh
}

// StreamChat sends a chat request and streams events. No tools are provided.
func StreamChat(ctx context.Context, ep Endpoint, model string, messages []ChatMessage, params ChatParams) <-chan StreamEvent {
	ch, resCh := streamChatRaw(ctx, ep, model, messages, params, nil)
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
		var usage *UsageStats
		if res, ok := <-resCh; ok {
			usage = res.Usage
		}
		out <- StreamEvent{Done: true, Usage: usage}
	}()
	return out
}
