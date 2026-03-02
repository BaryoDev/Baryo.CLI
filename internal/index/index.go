// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Index maintains a parsed symbol map of a project directory.
type Index struct {
	mu      sync.RWMutex
	root    string
	files   map[string]*FileSymbols
	ready   bool
	parsers map[string]*langParser
}

// New creates a new Index for the given project root.
func New(root string) *Index {
	return &Index{
		root: root,
		files: make(map[string]*FileSymbols),
		parsers: map[string]*langParser{
			"go":         newGoParser(),
			"javascript": newJSParser(),
			"typescript": newTSParser(),
			"python":     newPythonParser(),
		},
	}
}

// Build performs a full index of all source files in the project.
func (idx *Index) Build(ctx context.Context) error {
	files, err := DiscoverFiles(idx.root)
	if err != nil {
		return fmt.Errorf("discover files: %w", err)
	}

	newFiles := make(map[string]*FileSymbols, len(files))
	for _, rel := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fs, err := idx.parseOne(rel)
		if err != nil {
			continue // skip unparseable files
		}
		newFiles[rel] = fs
	}

	idx.mu.Lock()
	idx.files = newFiles
	idx.ready = true
	idx.mu.Unlock()

	return nil
}

// Update performs an incremental index update, only re-parsing files whose
// mtime has changed. It also removes files that no longer exist.
func (idx *Index) Update(ctx context.Context) error {
	files, err := DiscoverFiles(idx.root)
	if err != nil {
		return fmt.Errorf("discover files: %w", err)
	}

	// Build set of current files for deletion check.
	currentSet := make(map[string]bool, len(files))
	for _, rel := range files {
		currentSet[rel] = true
	}

	idx.mu.Lock()
	// Remove deleted files.
	for rel := range idx.files {
		if !currentSet[rel] {
			delete(idx.files, rel)
		}
	}
	idx.mu.Unlock()

	for _, rel := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		absPath := filepath.Join(idx.root, rel)
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}

		idx.mu.RLock()
		existing, ok := idx.files[rel]
		idx.mu.RUnlock()

		// Skip if mtime hasn't changed.
		if ok && existing.ModTime.Equal(info.ModTime()) {
			continue
		}

		fs, err := idx.parseOne(rel)
		if err != nil {
			continue
		}

		idx.mu.Lock()
		idx.files[rel] = fs
		idx.mu.Unlock()
	}

	return nil
}

// parseOne parses a single file and returns its symbols.
func (idx *Index) parseOne(rel string) (*FileSymbols, error) {
	absPath := filepath.Join(idx.root, rel)
	lang := LangForFile(rel)
	if lang == "" {
		return nil, fmt.Errorf("unknown language for %s", rel)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	fs, err := ParseFile(absPath, lang, content)
	if err != nil {
		return nil, err
	}
	fs.Path = rel // ensure relative path
	return fs, nil
}

// Ready returns true once the initial build has completed.
func (idx *Index) Ready() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.ready
}

// FileCount returns the number of indexed files.
func (idx *Index) FileCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.files)
}

// RepoMap generates a formatted repo map string within the given token budget.
// It applies progressive truncation when the full map exceeds the budget.
func (idx *Index) RepoMap(budgetTokens int) string {
	if budgetTokens <= 0 {
		return ""
	}

	idx.mu.RLock()
	// Copy file list under lock.
	sorted := make([]*FileSymbols, 0, len(idx.files))
	for _, fs := range idx.files {
		sorted = append(sorted, fs)
	}
	idx.mu.RUnlock()

	if len(sorted) == 0 {
		return ""
	}

	// Sort by path for stable output.
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	budgetChars := budgetTokens * 4 // rough chars-per-token estimate

	// Level 1: Full signatures
	if text := renderFiles(sorted, levelFull); len(text) <= budgetChars {
		return text
	}

	// Level 2: Top-level only (drop methods)
	if text := renderFiles(sorted, levelTopLevel); len(text) <= budgetChars {
		return text
	}

	// Level 3: Names only (no signatures)
	if text := renderFiles(sorted, levelNames); len(text) <= budgetChars {
		return text
	}

	// Level 4: File paths only
	if text := renderFiles(sorted, levelPaths); len(text) <= budgetChars {
		return text
	}

	// Level 5: Truncated file list
	return truncateFileList(sorted, budgetChars)
}

type detailLevel int

const (
	levelFull     detailLevel = iota // full signatures
	levelTopLevel                    // top-level symbols only
	levelNames                       // symbol names only (no params/types)
	levelPaths                       // file paths only
)

func renderFiles(files []*FileSymbols, level detailLevel) string {
	var b strings.Builder
	for i, fs := range files {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch level {
		case levelFull:
			b.WriteString(fs.Format())
		case levelTopLevel:
			b.WriteString(fs.FormatTopLevel())
		case levelNames:
			b.WriteString(fs.FormatShort())
		case levelPaths:
			b.WriteString(fs.Path)
		}
	}
	return b.String()
}

func truncateFileList(files []*FileSymbols, budgetChars int) string {
	var b strings.Builder
	for i, fs := range files {
		line := fs.Path + "\n"
		if b.Len()+len(line) > budgetChars-40 { // reserve space for "... and N more"
			remaining := len(files) - i
			fmt.Fprintf(&b, "... and %d more files", remaining)
			break
		}
		b.WriteString(line)
	}
	return b.String()
}
