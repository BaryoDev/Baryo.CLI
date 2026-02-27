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
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/logger"
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

// IsRemoteSocket returns true if the socket path is a TCP endpoint.
func IsRemoteSocket(socketPath string) bool {
	if strings.HasPrefix(socketPath, "tcp://") {
		return true
	}
	_, _, err := net.SplitHostPort(socketPath)
	return err == nil
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
	ToolCalls    []ToolCall  // accumulated tool calls (if any)
	Usage        *UsageStats // token usage from the final SSE frame (if reported)
	FinishReason string      // "stop", "length", "tool_calls", etc.
}

// streamChatRaw sends a chat request and streams events into the returned channel.
// The channel is closed when streaming ends. On completion the streamResult is
// sent via the result channel so callers can inspect accumulated tool calls.
func streamChatRaw(ctx context.Context, ep Endpoint, model string, messages []ChatMessage, params ChatParams, tools []ToolDefinition) (<-chan StreamEvent, <-chan streamResult) {
	logger.Debug("streamChatRaw", "provider", ep.Provider, "model", model, "messages", len(messages), "tools", len(tools))

	// Bedrock uses the AWS ConverseStream API — delegate to native adapter.
	if ep.Provider == "bedrock" {
		return streamChatBedrock(ctx, ep, model, messages, params, tools)
	}

	// Anthropic uses a completely different API format — delegate to native adapter.
	if ep.Provider == "anthropic" {
		return streamChatAnthropic(ctx, ep, model, messages, params, tools)
	}

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
			Stop:        params.Stop,
			Tools:       tools,
		}
		// top_k is not part of the OpenAI chat completions spec — only send
		// it for local models (Docker Model Runner / Ollama) which accept it.
		if !ep.IsRemote() {
			reqBody.TopK = params.TopK
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
		var finishReason string

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
				if choice.FinishReason != nil {
					finishReason = *choice.FinishReason
				}
				// Text content
				if choice.Delta.Content != "" {
					select {
					case ch <- StreamEvent{Token: choice.Delta.Content}:
					case <-ctx.Done():
						return
					}
				}

				// Tool call fragments.
				// Some providers (Gemini) send all parallel tool calls
				// with index 0. When a new function name appears at an
				// already-occupied index, allocate a fresh entry.
				for _, dtc := range choice.Delta.ToolCalls {
					idx := dtc.Index
					for idx >= len(toolAccs) {
						toolAccs = append(toolAccs, toolCallAcc{})
					}
					if dtc.Function != nil && dtc.Function.Name != "" &&
						toolAccs[idx].funcName != "" &&
						toolAccs[idx].funcName != dtc.Function.Name {
						idx = len(toolAccs)
						toolAccs = append(toolAccs, toolCallAcc{})
					}
					if dtc.ID != "" {
						toolAccs[idx].id = dtc.ID
					}
					if dtc.Function != nil {
						if dtc.Function.Name != "" {
							toolAccs[idx].funcName = dtc.Function.Name
						}
						toolAccs[idx].args.WriteString(dtc.Function.Arguments)
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

		resCh <- streamResult{ToolCalls: calls, Usage: lastUsage, FinishReason: finishReason}
	}()

	return ch, resCh
}

// maxContinuations is the maximum number of auto-continue rounds for
// responses truncated at max_tokens (finish_reason == "length").
const maxContinuations = 3

// StreamChat sends a chat request and streams events. No tools are provided.
// If the response is truncated (finish_reason == "length"), it auto-continues
// up to maxContinuations times.
func StreamChat(ctx context.Context, ep Endpoint, model string, messages []ChatMessage, params ChatParams) <-chan StreamEvent {
	out := make(chan StreamEvent, 64)
	go func() {
		defer close(out)

		msgs := make([]ChatMessage, len(messages))
		copy(msgs, messages)
		var lastUsage *UsageStats

		for attempt := 0; attempt <= maxContinuations; attempt++ {
			var contentBuf string
			ch, resCh := streamChatRaw(ctx, ep, model, msgs, params, nil)
			for evt := range ch {
				if evt.Token != "" {
					contentBuf += evt.Token
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			}
			res, ok := <-resCh
			if !ok {
				break
			}
			if res.Usage != nil {
				lastUsage = res.Usage
			}
			if res.FinishReason != "length" {
				break
			}
			// Truncated — append partial response and continue.
			msgs = append(msgs, NewChatMessage("assistant", contentBuf))
			msgs = append(msgs, NewChatMessage("user", "Continue"))
		}

		out <- StreamEvent{Done: true, Usage: lastUsage}
	}()
	return out
}
