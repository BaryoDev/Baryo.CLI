// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A tool round and the answer round are two requests, each billed for its own
// prompt. Done must carry both, or a run's cost is understated by every round
// but the last.
func TestToolLoopSumsUsageAcrossRounds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"echo","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
			io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}}`+"\n\n")
		} else {
			io.WriteString(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
			io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":150,"completion_tokens":20,"total_tokens":170}}`+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ep := Endpoint{BaseURL: srv.URL + "/v1", Provider: "openai", APIKey: "test"}
	defs := []ToolDefinition{{Type: "function", Function: FunctionDefinition{Name: "echo", Parameters: map[string]any{"type": "object"}}}}
	exec := func(ctx context.Context, name, args string) (string, bool) { return "ok", false }

	var got *UsageStats
	for evt := range StreamChatWithToolsN(context.Background(), ep, "gpt-test", []ChatMessage{NewChatMessage("user", "hi")}, ChatParams{}, defs, exec, 5) {
		if evt.Error != "" {
			t.Fatalf("stream error: %s", evt.Error)
		}
		if evt.Done {
			got = evt.Usage
		}
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("server saw %d requests, want 2 (tool round and answer round)", n)
	}
	if got == nil {
		t.Fatal("Done carried no usage")
	}
	if got.PromptTokens != 250 || got.CompletionTokens != 30 || got.TotalTokens != 280 {
		t.Errorf("usage = %+v, want the sum of both rounds {250 30 280}", *got)
	}
}

func TestAddUsageKeepsNilWhenNothingReported(t *testing.T) {
	if addUsage(nil, nil) != nil {
		t.Error("no usage from any round must stay nil, not become zeros")
	}
	u := &UsageStats{PromptTokens: 1}
	sum := addUsage(nil, u)
	sum.PromptTokens = 99
	if u.PromptTokens != 1 {
		t.Error("addUsage must not alias the provider's struct")
	}
}
