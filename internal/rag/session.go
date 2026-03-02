// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rag

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxSessions     = 20
	minExchangeLen  = 20 // skip very short Q&A pairs
)

// sessionFile is a minimal representation for loading session JSON.
type sessionFile struct {
	ID        string           `json:"id"`
	Messages  []sessionMessage `json:"messages"`
	CWD       string           `json:"cwd"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type sessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

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
		path      string
		cwd       string
		updatedAt time.Time
	}
	var metas []sessionMeta
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var sf sessionFile
		if err := json.Unmarshal(data, &sf); err != nil {
			continue
		}
		metas = append(metas, sessionMeta{
			path:      filepath.Join(dir, e.Name()),
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
		ss.loadSession(m.path)
	}
	return nil
}

// Chunks returns all indexed session chunks.
func (ss *SessionStore) Chunks() []Chunk {
	return ss.chunks
}

func (ss *SessionStore) loadSession(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return
	}

	// Extract user→assistant pairs, skipping tool/system messages.
	for i := 0; i < len(sf.Messages)-1; i++ {
		user := sf.Messages[i]
		if user.Role != "user" {
			continue
		}
		// Find the next assistant message.
		var assistant *sessionMessage
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

		userText := strings.TrimSpace(user.Content)
		assistText := strings.TrimSpace(assistant.Content)
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
