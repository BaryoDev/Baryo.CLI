// SPDX-License-Identifier: MIT

package export

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/plugin"
	"github.com/arnelirobles/baryo-cli/internal/session"
)

// readRecords parses an exported JSONL file into records.
func readRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("exported a line that is not JSON: %v\n%s", err, sc.Text())
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func eventsOf(records []map[string]any) []map[string]any {
	var events []map[string]any
	for _, r := range records {
		if r["record_type"] == "event" {
			events = append(events, r)
		}
	}
	return events
}

// A session with both archived and live messages exports all of them, archived first,
// because compaction appends what it is about to discard: everything archived happened
// before everything still in the conversation.
func TestBuiltinExportsArchivedThenLiveMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s, err := session.New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Archive([]llm.ChatMessage{
		llm.NewChatMessage("user", "why does it retry twice?"),
		llm.NewChatMessage("assistant", "checking the backoff loop"),
	}); err != nil {
		t.Fatal(err)
	}
	s.Messages = []llm.ChatMessage{
		llm.NewChatMessage("user", "what is left?"),
		llm.NewChatMessage("assistant", "just the test"),
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	dir, refs, err := Gather(time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("Gather returned %d sessions, want 1", len(refs))
	}
	if refs[0].ArchivePath == "" {
		t.Error("a session with an archive was handed to the exporter without its archive path")
	}
	if refs[0].TracePath != "" {
		t.Error("a session with no trace was given a trace path")
	}

	out := t.TempDir()
	resp, err := Builtin(plugin.ExportRequest{SessionsDir: dir, Sessions: refs, OutDir: out, BaryoVer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Sessions != 1 || resp.Events != 4 {
		t.Fatalf("response = %+v, want 1 session and 4 events", resp)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", resp.Warnings)
	}

	records := readRecords(t, resp.Written[0])
	if len(records) < 2 || records[0]["record_type"] != "manifest" || records[1]["record_type"] != "session" {
		t.Fatalf("export does not start with a manifest then a session: %+v", records)
	}
	if records[0]["schema_version"] != BuiltinSchema {
		t.Errorf("schema_version = %v", records[0]["schema_version"])
	}

	events := eventsOf(records)
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	wantOrigin := []string{"archived", "archived", "live", "live"}
	for i, e := range events {
		if e["origin"] != wantOrigin[i] {
			t.Errorf("event %d origin = %v, want %s", i, e["origin"], wantOrigin[i])
		}
		if idx, ok := e["event_index"].(float64); !ok || int(idx) != i {
			t.Errorf("event %d has event_index %v", i, e["event_index"])
		}
	}
	// An archived event publishes archived_at, the upper bound it actually knows, and says
	// so in time_fidelity. A live event has no time at all and must not be given one.
	// Nothing anywhere claims occurred_at: Baryo does not record when a message was sent,
	// and a field by that name would be read as though it did.
	if events[0]["archived_at"] == nil {
		t.Error("an archived event was exported without archived_at")
	}
	if got := events[0]["time_fidelity"]; got != "archived_upper_bound" {
		t.Errorf("archived event time_fidelity = %v, want archived_upper_bound", got)
	}
	if events[2]["archived_at"] != nil {
		t.Error("a live event was given an archived_at it does not have")
	}
	if got := events[2]["time_fidelity"]; got != "unknown" {
		t.Errorf("live event time_fidelity = %v, want unknown", got)
	}
	for i, e := range events {
		if _, claims := e["occurred_at"]; claims {
			t.Errorf("event %d claims occurred_at; no message in Baryo has a send time", i)
		}
	}
}

// Tool calls and results are the part of history a summary loses, so they must survive the
// export intact and be labelled.
func TestBuiltinPreservesToolCalls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s, err := session.New("m", "m:latest")
	if err != nil {
		t.Fatal(err)
	}
	call := llm.NewChatMessage("assistant", "reading the file")
	call.ToolCalls = []llm.ToolCall{{
		ID:       "call-1",
		Type:     "function",
		Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"main.go"}`},
	}}
	result := llm.NewChatMessage("tool", "package main")
	result.ToolCallID = "call-1"
	if err := s.Archive([]llm.ChatMessage{call, result}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	dir, refs, err := Gather(time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	resp, err := Builtin(plugin.ExportRequest{SessionsDir: dir, Sessions: refs, OutDir: out})
	if err != nil {
		t.Fatal(err)
	}

	events := eventsOf(readRecords(t, resp.Written[0]))
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0]["event_type"] != "tool_call" {
		t.Errorf("first event_type = %v, want tool_call", events[0]["event_type"])
	}
	if events[1]["event_type"] != "tool_result" {
		t.Errorf("second event_type = %v, want tool_result", events[1]["event_type"])
	}
	// The arguments have to survive, not just the fact that a call happened.
	msg, _ := events[0]["message"].(map[string]any)
	calls, _ := msg["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool_calls did not survive: %+v", msg)
	}
	fn, _ := calls[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "read_file" || !strings.Contains(fn["arguments"].(string), "main.go") {
		t.Errorf("tool call detail did not survive: %+v", fn)
	}
}

// A pre-envelope archive has no timestamps. The export says so rather than inventing them,
// because an importer can decide what to do with an undated record and cannot undo a
// fabricated one.
func TestBuiltinWarnsAboutUndatedArchives(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s, err := session.New("m", "m:latest")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(llm.NewChatMessage("user", "from an older build"))
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(home, ".baryo", "sessions", s.ID+".archive.jsonl")
	if err := os.WriteFile(archive, append(legacy, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	dir, refs, err := Gather(time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := Builtin(plugin.ExportRequest{SessionsDir: dir, Sessions: refs, OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "predate archive timestamps") {
		t.Errorf("warnings = %v, want one about undated records", resp.Warnings)
	}
	events := eventsOf(readRecords(t, resp.Written[0]))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0]["archived_at"] != nil {
		t.Error("an undated archived message was given a timestamp")
	}
	if got := events[0]["time_fidelity"]; got != "unknown" {
		t.Errorf("undated record time_fidelity = %v, want unknown", got)
	}
}

func TestGatherFiltersBySinceAndSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	old, err := session.New("m", "m:latest")
	if err != nil {
		t.Fatal(err)
	}
	old.Messages = []llm.ChatMessage{llm.NewChatMessage("user", "old")}
	if err := old.Save(); err != nil {
		t.Fatal(err)
	}
	// Save() stamps UpdatedAt, so reach past it to age this one deliberately.
	old.UpdatedAt = time.Now().AddDate(0, 0, -30)
	data, err := json.MarshalIndent(old, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path, err := session.FilePath(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	recent, err := session.New("m", "m:latest")
	if err != nil {
		t.Fatal(err)
	}
	recent.Messages = []llm.ChatMessage{llm.NewChatMessage("user", "recent")}
	if err := recent.Save(); err != nil {
		t.Fatal(err)
	}

	if _, refs, err := Gather(time.Time{}, ""); err != nil {
		t.Fatal(err)
	} else if len(refs) != 2 {
		t.Errorf("unfiltered Gather returned %d sessions, want 2", len(refs))
	}

	cutoff := time.Now().AddDate(0, 0, -7)
	_, refs, err := Gather(cutoff, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ID != recent.ID {
		t.Errorf("--since returned %+v, want only the recent session", refs)
	}

	_, refs, err = Gather(time.Time{}, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ID != old.ID {
		t.Errorf("--session returned %+v, want only the named session", refs)
	}
}

// Re-exporting must replace the previous file, not append to it: an export that doubles
// every record each time it runs is worse than one that fails.
func TestBuiltinOverwritesPreviousExport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.New("m", "m:latest")
	if err != nil {
		t.Fatal(err)
	}
	s.Messages = []llm.ChatMessage{llm.NewChatMessage("user", "once")}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	dir, refs, err := Gather(time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	req := plugin.ExportRequest{SessionsDir: dir, Sessions: refs, OutDir: out}

	first, err := Builtin(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Builtin(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Events != second.Events {
		t.Errorf("event count changed between runs: %d then %d", first.Events, second.Events)
	}
	if got := len(eventsOf(readRecords(t, second.Written[0]))); got != 1 {
		t.Errorf("re-export left %d events in the file, want 1", got)
	}
}
