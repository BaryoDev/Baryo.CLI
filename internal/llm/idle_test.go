package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stalledServer sends headers and one SSE frame, then goes silent without
// closing the connection. Headers arrive, so ResponseHeaderTimeout never fires:
// before the idle watchdog this hung until the process was killed.
func stalledServer(t *testing.T) *httptest.Server {
	t.Helper()
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-block
	}))
	t.Cleanup(func() {
		close(block)
		srv.Close()
	})
	return srv
}

func TestStreamChatReportsIdleProvider(t *testing.T) {
	srv := stalledServer(t)

	old := StreamIdleTimeout
	StreamIdleTimeout = 300 * time.Millisecond
	t.Cleanup(func() { StreamIdleTimeout = old })

	ep := Endpoint{BaseURL: srv.URL + "/v1", Provider: "openai", APIKey: "test"}
	ch := StreamChat(context.Background(), ep, "gpt-test", []ChatMessage{NewChatMessage("user", "hi")}, ChatParams{})

	var tokens, errText string
	deadline := time.After(10 * time.Second)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				if errText == "" {
					t.Fatalf("stream closed with no error event (tokens=%q)", tokens)
				}
				if !strings.Contains(errText, "no data") {
					t.Errorf("error = %q, want it to name the idle provider", errText)
				}
				if tokens != "partial" {
					t.Errorf("tokens = %q, want the frame received before the stall", tokens)
				}
				return
			}
			tokens += evt.Token
			if evt.Error != "" {
				errText = evt.Error
			}
		case <-deadline:
			t.Fatal("stream never ended: the idle watchdog did not fire")
		}
	}
}

// A stream that keeps sending must not be cut off by the watchdog.
func TestStreamChatDoesNotTimeOutWhileDataFlows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, _ := w.(http.Flusher)
		for i := 0; i < 6; i++ {
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			if f != nil {
				f.Flush()
			}
			time.Sleep(120 * time.Millisecond)
		}
		io.WriteString(w, "data: [DONE]\n\n")
		if f != nil {
			f.Flush()
		}
	}))
	defer srv.Close()

	old := StreamIdleTimeout
	StreamIdleTimeout = 300 * time.Millisecond
	t.Cleanup(func() { StreamIdleTimeout = old })

	ep := Endpoint{BaseURL: srv.URL + "/v1", Provider: "openai", APIKey: "test"}
	ch := StreamChat(context.Background(), ep, "gpt-test", []ChatMessage{NewChatMessage("user", "hi")}, ChatParams{})

	var tokens string
	for evt := range ch {
		if evt.Error != "" {
			t.Fatalf("unexpected error while data was flowing: %s", evt.Error)
		}
		tokens += evt.Token
	}
	if tokens != "xxxxxx" {
		t.Errorf("tokens = %q, want all six frames", tokens)
	}
}
