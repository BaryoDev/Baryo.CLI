// SPDX-License-Identifier: MIT

package rag

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/session"
)

const (
	maxSessions    = 20
	minExchangeLen = 20 // skip very short Q&A pairs
)

// SessionStore indexes past conversations for RAG retrieval.
type SessionStore struct {
	cwd    string
	chunks []Chunk
}

// NewSessionStore creates a session store that prioritizes sessions from cwd.
func NewSessionStore(cwd string) *SessionStore {
	return &SessionStore{cwd: cwd}
}

// Build loads recent sessions and extracts Q&A chunks.
func (ss *SessionStore) Build(ctx context.Context) error {
	ss.chunks = nil

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".baryo", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // no sessions yet — not an error
	}

	// Load all session metadata for sorting.
	type sessionMeta struct {
		id        string
		cwd       string
		updatedAt time.Time
	}
	var metas []sessionMeta
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		// session.Load replays the journal, which <id>.json alone does not hold.
		sf, err := session.Load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		metas = append(metas, sessionMeta{
			id:        sf.ID,
			cwd:       sf.CWD,
			updatedAt: sf.UpdatedAt,
		})
	}

	// Sort: CWD-matching first, then by recency.
	sort.Slice(metas, func(i, j int) bool {
		iMatch := metas[i].cwd == ss.cwd
		jMatch := metas[j].cwd == ss.cwd
		if iMatch != jMatch {
			return iMatch
		}
		return metas[i].updatedAt.After(metas[j].updatedAt)
	})

	// Take up to maxSessions.
	if len(metas) > maxSessions {
		metas = metas[:maxSessions]
	}

	for _, m := range metas {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ss.loadSession(m.id)
	}
	return nil
}

// Chunks returns all indexed session chunks.
func (ss *SessionStore) Chunks() []Chunk {
	return ss.chunks
}

func (ss *SessionStore) loadSession(id string) {
	sf, err := session.Load(id)
	if err != nil {
		return
	}
	text := func(m llm.ChatMessage) string {
		if m.Content == nil {
			return ""
		}
		return *m.Content
	}

	// Extract user→assistant pairs, skipping tool/system messages.
	for i := 0; i < len(sf.Messages)-1; i++ {
		user := sf.Messages[i]
		if user.Role != "user" {
			continue
		}
		// Find the next assistant message.
		var assistant *llm.ChatMessage
		for j := i + 1; j < len(sf.Messages); j++ {
			if sf.Messages[j].Role == "assistant" {
				assistant = &sf.Messages[j]
				break
			}
			if sf.Messages[j].Role == "user" {
				break // no assistant reply for this user message
			}
		}
		if assistant == nil {
			continue
		}

		userText := strings.TrimSpace(text(user))
		assistText := strings.TrimSpace(text(*assistant))
		if len(userText)+len(assistText) < minExchangeLen {
			continue
		}

		// Truncate long assistant responses to keep chunks reasonable.
		if len(assistText) > maxChunkSize {
			assistText = assistText[:maxChunkSize]
		}

		text := "Q: " + userText + "\nA: " + assistText
		ss.chunks = append(ss.chunks, Chunk{
			Source: sf.ID,
			Text:   text,
			Type:   "session",
		})
	}
}
