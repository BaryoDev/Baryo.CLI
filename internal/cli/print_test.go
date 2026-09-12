package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/mcp"
)

// Headless mode blocks destructive tools without --yolo. Since IsDestructive
// fails closed, an unregistered name would be reported as needing --yolo, which
// is misleading: no flag can make a tool that does not exist run.
func TestHeadlessExecutorRejectsUnknownTool(t *testing.T) {
	out, isErr := makeHeadlessExecutor("confirm", nil, nil)(context.Background(), "tool_that_does_not_exist", "{}")
	if !isErr {
		t.Error("want an error result")
	}
	if !strings.Contains(out, "unknown tool") {
		t.Errorf("got %q, want it to name the tool as unknown", out)
	}
}

type fakeMCP struct {
	readOnly map[string]bool
	executed []string
}

func (f *fakeMCP) CompactToolDefinitions([]string, int) []llm.ToolDefinition { return nil }
func (f *fakeMCP) Execute(_ context.Context, name, _ string) (string, bool) {
	f.executed = append(f.executed, name)
	return "mcp ok", false
}
func (f *fakeMCP) IsMCPTool(name string) bool      { return strings.HasPrefix(name, "mcp__") }
func (f *fakeMCP) IsReadOnlyTool(name string) bool { return f.readOnly[name] }

// Headless blocks native destructive tools without --yolo. MCP tools used to
// skip that check entirely.
func TestHeadlessExecutorGatesNonReadOnlyMCPTool(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{"mcp__search__query": true}}
	exec := makeHeadlessExecutor("confirm", mgr, nil)

	out, isErr := exec(context.Background(), "mcp__fs__write", "{}")
	if !isErr || !strings.Contains(out, "--yolo") {
		t.Errorf("got %q (isErr=%v), want it blocked pending --yolo", out, isErr)
	}
	if len(mgr.executed) != 0 {
		t.Errorf("tool ran while blocked: %v", mgr.executed)
	}

	if out, isErr := exec(context.Background(), "mcp__search__query", "{}"); isErr {
		t.Errorf("read-only MCP tool should run in headless mode, got %q", out)
	}
}

func TestHeadlessExecutorRunsAnyMCPToolInAutoMode(t *testing.T) {
	mgr := &fakeMCP{readOnly: map[string]bool{}}
	if out, isErr := makeHeadlessExecutor("auto", mgr, nil)(context.Background(), "mcp__fs__write", "{}"); isErr {
		t.Errorf("auto mode should run it, got %q", out)
	}
}

// When the overall deadline fires, the stream's error event is dropped on
// purpose: the send would block on a context that is already done. So print mode
// has to notice the deadline itself, or a timed-out run exits 0 and a CI job
// reports success for work that never finished.
func TestPrintTextReturnsErrorOnTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"stalling\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-block
	}))
	t.Cleanup(func() {
		close(block)
		srv.Close()
	})

	code := runPrintText(PrintOptions{
		Endpoint: llm.Endpoint{BaseURL: srv.URL + "/v1", Provider: "openai", APIKey: "test"},
		Model:    llm.Model{Tag: "test-model"},
		Prompt:   "hi",
		Timeout:  400 * time.Millisecond,
	})
	if code == 0 {
		t.Error("a run cut short by --timeout must not report success")
	}
}

// JSON mode has its own no-tools branch with its own return, which is how the
// first fix missed it. Both JSON exits must report a cut-short run.
func TestPrintJSONReturnsErrorOnTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"stalling\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-block
	}))
	t.Cleanup(func() {
		close(block)
		srv.Close()
	})

	opts := PrintOptions{
		Endpoint: llm.Endpoint{BaseURL: srv.URL + "/v1", Provider: "openai", APIKey: "test"},
		Model:    llm.Model{Tag: "test-model"},
		Prompt:   "hi",
		Timeout:  400 * time.Millisecond,
	}
	if code := runPrintJSON(opts); code == 0 {
		t.Error("json mode without tools must not report success after a timeout")
	}

	opts.EnableTools = true
	if code := runPrintJSON(opts); code == 0 {
		t.Error("json mode with tools must not report success after a timeout")
	}
}

// main.go declares `var mcpMgr *mcp.Manager` and passes it into this interface
// field whether or not a server was configured. A typed nil in an interface is
// not a nil interface, so the `!= nil` guard passes and the method runs on a nil
// receiver. This crashed every headless run with no MCP servers.
func TestHeadlessSurvivesTypedNilMCPManager(t *testing.T) {
	var typedNil *mcp.Manager
	opts := PrintOptions{
		Endpoint:    llm.Endpoint{BaseURL: "http://127.0.0.1:1/v1", Provider: "openai"},
		Model:       llm.Model{Tag: "test-model"},
		Prompt:      "hi",
		EnableTools: true,
		MCPManager:  typedNil,
		Timeout:     300 * time.Millisecond,
	}
	// Connecting to port 1 fails fast; the point is that building the tool
	// definitions does not panic first.
	if code := runPrintText(opts); code == 0 {
		t.Error("expected a non-zero exit from an unreachable endpoint")
	}
}
