// SPDX-License-Identifier: MIT

package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// setTestHome points the sessions dir at a temp directory for the test.
func setTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestArchiveAndLoadArchive(t *testing.T) {
	setTestHome(t)
	s, err := New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Archive(nil); err != nil {
		t.Fatalf("Archive(nil) = %v, want nil", err)
	}
	if msgs, err := LoadArchive(s.ID); err != nil || msgs != nil {
		t.Fatalf("LoadArchive with no archive = %v, %v; want nil, nil", msgs, err)
	}

	batch1 := []llm.ChatMessage{
		llm.NewChatMessage("user", "how do I configure the widget?"),
		llm.NewChatMessage("assistant", "set widget.enabled in config.toml"),
	}
	batch2 := []llm.ChatMessage{
		llm.NewChatMessage("user", "now it crashes on startup"),
	}
	if err := s.Archive(batch1); err != nil {
		t.Fatal(err)
	}
	if err := s.Archive(batch2); err != nil {
		t.Fatal(err)
	}

	msgs, err := LoadArchive(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("LoadArchive returned %d messages, want 3", len(msgs))
	}
	if *msgs[0].Content != "how do I configure the widget?" {
		t.Errorf("first archived message = %q", *msgs[0].Content)
	}
	if msgs[2].Role != "user" || *msgs[2].Content != "now it crashes on startup" {
		t.Errorf("appended batch not preserved: %+v", msgs[2])
	}
}

func TestSearchIncludesArchivedMessages(t *testing.T) {
	setTestHome(t)
	s, err := New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}
	// Live messages simulate a post-compaction session: summary only.
	s.Messages = []llm.ChatMessage{
		llm.NewChatMessage("user", "[Conversation summary]\n\nWorked on the parser."),
		llm.NewChatMessage("assistant", "Understood."),
	}
	s.Title = "parser work"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// Archived content that no longer exists in the live message list.
	archived := []llm.ChatMessage{
		llm.NewChatMessage("user", "the tokenizer breaks on emoji input"),
	}
	if err := s.Archive(archived); err != nil {
		t.Fatal(err)
	}

	results, err := Search("tokenizer breaks on emoji")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != s.ID {
		t.Fatalf("Search over archive = %+v, want session %s", results, s.ID)
	}

	if results, _ := Search("no such phrase anywhere"); len(results) != 0 {
		t.Fatalf("Search miss returned %+v, want none", results)
	}
}

func TestCleanOldRemovesArchive(t *testing.T) {
	home := setTestHome(t)
	s, err := New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}
	s.Messages = []llm.ChatMessage{llm.NewChatMessage("user", "hello")}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Archive(s.Messages); err != nil {
		t.Fatal(err)
	}

	// Backdate the session file so CleanOld considers it stale.
	dir := filepath.Join(home, ".baryo", "sessions")
	loaded, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded.UpdatedAt = time.Now().AddDate(0, 0, -60)
	data, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, s.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	deleted, err := CleanOld(30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("CleanOld deleted %d sessions, want 1", deleted)
	}
	if _, err := os.Stat(archivePath(dir, s.ID)); !os.IsNotExist(err) {
		t.Errorf("archive file still exists after CleanOld")
	}
}

// An archived record carries when it was archived and where it sits in the sequence.
// Without both, an exporter cannot say when anything happened and cannot order records
// once a corrupt line has been skipped.
func TestArchiveRecordsCarryTimestampAndSequence(t *testing.T) {
	setTestHome(t)
	s, err := New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Add(-time.Second)
	if err := s.Archive([]llm.ChatMessage{
		llm.NewChatMessage("user", "first"),
		llm.NewChatMessage("assistant", "second"),
	}); err != nil {
		t.Fatal(err)
	}
	// A second call must continue the sequence, not restart it.
	if err := s.Archive([]llm.ChatMessage{llm.NewChatMessage("user", "third")}); err != nil {
		t.Fatal(err)
	}

	records, err := LoadArchiveRecords(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}
	after := time.Now().UTC().Add(time.Second)
	for i, rec := range records {
		if rec.Seq != i {
			t.Errorf("record %d has Seq %d, want %d", i, rec.Seq, i)
		}
		if rec.At.Before(before) || rec.At.After(after) {
			t.Errorf("record %d timestamp %v is outside the window %v..%v", i, rec.At, before, after)
		}
	}
	if got := *records[2].Msg.Content; got != "third" {
		t.Errorf("third record content = %q", got)
	}
}

// Sequence numbering has to survive the process, because a session can be resumed. It is
// counted from the file for that reason; a counter on the Session would restart at zero
// and silently produce two records claiming the same position.
func TestArchiveSequenceSurvivesReload(t *testing.T) {
	setTestHome(t)
	s, err := New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Archive([]llm.ChatMessage{llm.NewChatMessage("user", "before")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Archive([]llm.ChatMessage{llm.NewChatMessage("user", "after")}); err != nil {
		t.Fatal(err)
	}

	records, err := LoadArchiveRecords(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0].Seq != 0 || records[1].Seq != 1 {
		t.Errorf("sequence restarted across reload: got %d then %d", records[0].Seq, records[1].Seq)
	}
}

// Archives written before the envelope existed hold bare llm.ChatMessage objects. They
// must still read back, still be searchable, and must not be given an invented timestamp.
func TestLoadArchiveReadsPreEnvelopeLines(t *testing.T) {
	home := setTestHome(t)
	s, err := New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Exactly what the old Archive wrote: one bare message per line, no envelope.
	legacy := llm.NewChatMessage("user", "the widget used to misbehave")
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".baryo", "sessions")
	path := filepath.Join(dir, s.ID+".archive.jsonl")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := LoadArchiveRecords(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records from a legacy archive, want 1", len(records))
	}
	if got := *records[0].Msg.Content; got != "the widget used to misbehave" {
		t.Errorf("legacy content = %q", got)
	}
	if !records[0].At.IsZero() {
		t.Errorf("legacy record got timestamp %v, want the zero time rather than an invented one", records[0].At)
	}

	// Still searchable, which is what the archive is read for today.
	results, err := Search("misbehave")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Search over a legacy archive returned %d results, want 1", len(results))
	}

	// And a new record appended after legacy lines continues past them.
	if err := s.Archive([]llm.ChatMessage{llm.NewChatMessage("user", "new")}); err != nil {
		t.Fatal(err)
	}
	records, err = LoadArchiveRecords(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records after appending, want 2", len(records))
	}
	if records[1].Seq != 1 {
		t.Errorf("record appended after a legacy line has Seq %d, want 1", records[1].Seq)
	}
}
