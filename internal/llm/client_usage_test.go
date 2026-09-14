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

// A reply cut off at max_tokens is continued with a second request. Both are
// billed, so Done must carry the sum.
func TestStreamChatSumsUsageAcrossContinuations(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			io.WriteString(w, `data: {"choices":[{"delta":{"content":"first half "},"finish_reason":"length"}]}`+"\n\n")
			io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":8,"total_tokens":48}}`+"\n\n")
		} else {
			io.WriteString(w, `data: {"choices":[{"delta":{"content":"and the second"},"finish_reason":"stop"}]}`+"\n\n")
			io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":60,"completion_tokens":5,"total_tokens":65}}`+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ep := Endpoint{BaseURL: srv.URL + "/v1", Provider: "openai", APIKey: "test"}
	var got *UsageStats
	for evt := range StreamChat(context.Background(), ep, "gpt-test", []ChatMessage{NewChatMessage("user", "hi")}, ChatParams{}) {
		if evt.Error != "" {
			t.Fatalf("stream error: %s", evt.Error)
		}
		if evt.Done {
			got = evt.Usage
		}
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("server saw %d requests, want 2 (reply and continuation)", n)
	}
	if got == nil || got.PromptTokens != 100 || got.CompletionTokens != 13 || got.TotalTokens != 113 {
		t.Errorf("usage = %+v, want the sum of both requests {100 13 113}", got)
	}
}
