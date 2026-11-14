// SPDX-License-Identifier: MIT

package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/session"
)

// A turn saved to the session journal is not in <id>.json yet. The session store must
// still index it.
func TestSessionStoreIndexesJournaledTurns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := session.New("m", "m:1")
	if err != nil {
		t.Fatal(err)
	}
	s.Messages = []llm.ChatMessage{
		llm.NewChatMessage("user", "how do I configure the widget?"),
		llm.NewChatMessage("assistant", "set widget.enabled in the config file"),
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s.Messages = append(s.Messages,
		llm.NewChatMessage("user", "and how do I restart the gadget service?"),
		llm.NewChatMessage("assistant", "run the restart command for the gadget"),
	)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	ss := NewSessionStore(s.CWD)
	if err := ss.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range ss.Chunks() {
		if strings.Contains(c.Text, "gadget") {
			found = true
		}
	}
	if !found || len(ss.Chunks()) != 2 {
		t.Fatalf("indexed %d chunks, journaled turn found = %v", len(ss.Chunks()), found)
	}
}
