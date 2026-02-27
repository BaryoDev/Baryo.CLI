// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// Session represents a saved conversation.
type Session struct {
	ID        string                 `json:"id"`
	ModelName string                 `json:"model_name"`
	ModelTag  string                 `json:"model_tag"`
	Messages  []llm.ChatMessage   `json:"messages"`
	CWD       string                 `json:"cwd"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// sessionsDir returns ~/.baryo/sessions/, creating it if needed.
func sessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".baryo", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// New creates a new session with a random ID and the current working directory.
func New(modelName, modelTag string) (*Session, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	now := time.Now()
	return &Session{
		ID:        id,
		ModelName: modelName,
		ModelTag:  modelTag,
		CWD:       cwd,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Save writes the session to disk as JSON.
func (s *Session) Save() error {
	dir, err := sessionsDir()
	if err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, s.ID+".json"), data, 0o600)
}

// Load reads a session by ID from disk.
func Load(id string) (*Session, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("corrupt session %q: %w", id, err)
	}
	return &s, nil
}

// Summary is a lightweight view of a session for listing.
type Summary struct {
	ID        string
	ModelName string
	Messages  int
	UpdatedAt time.Time
	CWD       string
}

// List returns summaries of all saved sessions, sorted by most recently updated.
func List() ([]Summary, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var summaries []Summary
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		summaries = append(summaries, Summary{
			ID:        s.ID,
			ModelName: s.ModelName,
			Messages:  len(s.Messages),
			UpdatedAt: s.UpdatedAt,
			CWD:       s.CWD,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

// LatestForDir returns the most recently updated session whose CWD matches dir.
func LatestForDir(dir string) (*Session, error) {
	summaries, err := List()
	if err != nil {
		return nil, err
	}
	for _, s := range summaries {
		if s.CWD == dir {
			return Load(s.ID)
		}
	}
	return nil, fmt.Errorf("no session found for %s", dir)
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
