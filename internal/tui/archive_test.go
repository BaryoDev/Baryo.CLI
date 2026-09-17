// SPDX-License-Identifier: MIT

package tui

import (
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/session"
)

// Conversation compaction archived the messages it replaced, and for a while it was the
// only path that did. /clear dropped the whole conversation and the post-summary
// compactions shrank bulky messages in place, so that content was gone for good — which
// is precisely the evidence /sessions search and any history exporter need back.

func archiveTestModel(t *testing.T) *ChatModel {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	sess, err := session.New("test-model", "test:latest")
	if err != nil {
		t.Fatal(err)
	}
	return &ChatModel{session: sess}
}

// /clear starts a fresh session. It must not take the old conversation with it.
func TestClearArchivesTheConversation(t *testing.T) {
	m := archiveTestModel(t)
	oldID := m.session.ID
	m.messages = []llm.ChatMessage{
		llm.NewChatMessage("user", "why does the widget retry twice?"),
		llm.NewChatMessage("assistant", "because the backoff loop double-counts"),
	}

	got, _ := m.handleCommand("/clear")

	if got.session == nil || got.session.ID == oldID {
		t.Fatalf("/clear did not start a new session (old %q, new %v)", oldID, got.session)
	}
	if len(got.messages) != 0 {
		t.Errorf("/clear left %d messages in the conversation", len(got.messages))
	}

	// The records must land on the session the messages belonged to, not the new one.
	records, err := session.LoadArchiveRecords(oldID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("archived %d messages on the cleared session, want 2", len(records))
	}
	if got := *records[0].Msg.Content; got != "why does the widget retry twice?" {
		t.Errorf("first archived message = %q", got)
	}
	if records[0].At.IsZero() {
		t.Error("archived record has no timestamp")
	}
	if newRecords, err := session.LoadArchiveRecords(got.session.ID); err != nil {
		t.Fatal(err)
	} else if len(newRecords) != 0 {
		t.Errorf("the new session starts with %d archived records, want 0", len(newRecords))
	}
}

// Searching for content that has been cleared away is the whole point of archiving it.
func TestClearedConversationStaysSearchable(t *testing.T) {
	m := archiveTestModel(t)
	m.messages = []llm.ChatMessage{llm.NewChatMessage("user", "the backoff loop double-counts")}
	if err := m.session.Save(); err != nil {
		t.Fatal(err)
	}

	if _, _ = m.handleCommand("/clear"); true {
		results, err := session.Search("double-counts")
		if err != nil {
			t.Fatal(err)
		}
		if len(results) == 0 {
			t.Error("content cleared by /clear is no longer searchable")
		}
	}
}

// archiveAway is best-effort: a session that does not exist yet, or nothing to archive,
// must not be an error and must not panic. Compaction calls this on a path the user is
// already waiting on.
func TestArchiveAwayHandlesNothingToDo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	noSession := &ChatModel{}
	noSession.archiveAway([]llm.ChatMessage{llm.NewChatMessage("user", "x")}, "no session")

	m := archiveTestModel(t)
	m.archiveAway(nil, "nil messages")
	m.archiveAway([]llm.ChatMessage{}, "empty messages")

	records, err := session.LoadArchiveRecords(m.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("archived %d records from nothing", len(records))
	}
}
