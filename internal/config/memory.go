// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Memory is a single stored fact.
type Memory struct {
	Fact      string    `json:"fact"`
	CreatedAt time.Time `json:"created_at"`
	Source    string    `json:"source"` // "global" or "project"
}

// MemoryStore is the JSON wrapper for a list of memories.
type MemoryStore struct {
	Memories []Memory `json:"memories"`
}

const maxMemoriesPerScope = 50

// globalMemoryPath returns ~/.baryo/memories.json.
func globalMemoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".baryo", "memories.json")
}

// projectMemoryPath returns .baryo/memories.json.
func projectMemoryPath() string {
	return filepath.Join(".baryo", "memories.json")
}

// loadMemoryFile reads a memories.json file and returns its memories.
func loadMemoryFile(path, source string) []Memory {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var store MemoryStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil
	}
	// Ensure source is set correctly
	for i := range store.Memories {
		store.Memories[i].Source = source
	}
	return store.Memories
}

// saveMemoryFile writes memories to a JSON file, creating dirs as needed.
func saveMemoryFile(path string, memories []Memory) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	store := MemoryStore{Memories: memories}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadMemories reads both global and project memory files and returns
// all memories with project memories first.
func LoadMemories() []Memory {
	project := loadMemoryFile(projectMemoryPath(), "project")
	global := loadMemoryFile(globalMemoryPath(), "global")
	return append(project, global...)
}

// AddMemory appends a fact to the appropriate memory file.
// If global is true, saves to ~/.baryo/memories.json; otherwise .baryo/memories.json.
// Caps at maxMemoriesPerScope per file.
func AddMemory(fact string, global bool) error {
	path := projectMemoryPath()
	source := "project"
	if global {
		path = globalMemoryPath()
		source = "global"
	}

	existing := loadMemoryFile(path, source)
	if len(existing) >= maxMemoriesPerScope {
		return errMemoryLimitReached
	}

	existing = append(existing, Memory{
		Fact:      fact,
		CreatedAt: time.Now(),
		Source:    source,
	})
	return saveMemoryFile(path, existing)
}

// RemoveMemory removes the first memory matching the substring (case-insensitive).
// Returns the removed fact text and nil error, or empty string and error if not found.
func RemoveMemory(substring string) (string, error) {
	lower := strings.ToLower(substring)

	// Try project first, then global
	for _, scope := range []struct {
		path   string
		source string
	}{
		{projectMemoryPath(), "project"},
		{globalMemoryPath(), "global"},
	} {
		memories := loadMemoryFile(scope.path, scope.source)
		for i, mem := range memories {
			if strings.Contains(strings.ToLower(mem.Fact), lower) {
				removed := mem.Fact
				memories = append(memories[:i], memories[i+1:]...)
				if err := saveMemoryFile(scope.path, memories); err != nil {
					return "", err
				}
				return removed, nil
			}
		}
	}
	return "", errMemoryNotFound
}

// ListMemories returns memories separated by scope.
func ListMemories() (global []Memory, project []Memory) {
	project = loadMemoryFile(projectMemoryPath(), "project")
	global = loadMemoryFile(globalMemoryPath(), "global")
	if project == nil {
		project = []Memory{}
	}
	if global == nil {
		global = []Memory{}
	}
	return global, project
}

// FormatMemoriesForPrompt formats memories for injection into the system prompt.
func FormatMemoriesForPrompt(memories []Memory) string {
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<memories>\n")
	for _, mem := range memories {
		b.WriteString("- ")
		b.WriteString(mem.Fact)
		b.WriteString("\n")
	}
	b.WriteString("</memories>")
	return b.String()
}

// Sentinel errors for memory operations.
type memoryError string

func (e memoryError) Error() string { return string(e) }

const (
	errMemoryLimitReached memoryError = "memory limit reached (max 50 per scope)"
	errMemoryNotFound     memoryError = "no matching memory found"
)
