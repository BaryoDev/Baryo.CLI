// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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
