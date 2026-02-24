// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/arnelirobles/baryo-cli/internal/tools"
)

const (
	maxMentionFileSize = 100 * 1024 // 100 KB
	maxCompletions     = 50
)

// mentionCompletion tracks the state of @-mention tab completion.
type mentionCompletion struct {
	active     bool
	prefix     string   // the partial text after @
	candidates []string // matched file paths
	index      int      // current selection in candidates
	startPos   int      // position of @ in the text
}

// fileContext holds the content of an @-mentioned file.
type fileContext struct {
	path    string
	content string
	lines   int
}

// textareaCursorPos returns the absolute cursor position in the textarea value.
func textareaCursorPos(value string, line int, col int) int {
	lines := strings.Split(value, "\n")
	pos := 0
	for i := 0; i < line && i < len(lines); i++ {
		pos += len(lines[i]) + 1 // +1 for newline
	}
	pos += col
	return pos
}

// findMentionAtCursor scans backward from cursor to find an @ preceded by
// whitespace or start-of-string. Returns the start position, the partial
// text after @, and whether a mention was found.
func findMentionAtCursor(text string, cursorPos int) (start int, partial string, found bool) {
	if cursorPos > len(text) {
		cursorPos = len(text)
	}

	// Find the @ sign by scanning backward from cursor
	for i := cursorPos - 1; i >= 0; i-- {
		ch := text[i]
		// Stop at whitespace — no @ found in this word
		if ch == ' ' || ch == '\t' || ch == '\n' {
			return 0, "", false
		}
		if ch == '@' {
			// @ must be at start of string or preceded by whitespace
			if i > 0 {
				prev := text[i-1]
				if prev != ' ' && prev != '\t' && prev != '\n' {
					return 0, "", false
				}
			}
			return i, text[i+1 : cursorPos], true
		}
	}
	return 0, "", false
}

// globCompletions returns file paths matching the partial prefix.
// Directories get a trailing /, .git/ and gitignored files are filtered out.
//
// When partial has no slash (e.g. "ma"), it matches both top-level ("ma*")
// and recursively ("**/ma*") so deeper files like internal/match.go appear.
// When partial has a slash (e.g. "internal/tui/c"), it matches within that path.
func globCompletions(partial string) []string {
	patterns := []string{partial + "*"}

	// If no directory separator, also search recursively
	if !strings.Contains(partial, "/") && partial != "" {
		patterns = append(patterns, "**/"+partial+"*")
	}

	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var results []string

	for _, pattern := range patterns {
		matches, err := doublestar.Glob(os.DirFS("."), pattern)
		if err != nil {
			continue
		}

		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true

			// Skip .git internals
			if m == ".git" || strings.HasPrefix(m, ".git/") || strings.HasPrefix(m, ".git"+string(filepath.Separator)) {
				continue
			}

			absPath := filepath.Join(cwd, m)

			if tools.IsGitIgnored(ctx, absPath) {
				continue
			}

			fi, err := os.Stat(absPath)
			if err != nil {
				continue
			}
			if fi.IsDir() {
				results = append(results, m+"/")
			} else {
				results = append(results, m)
			}

			if len(results) >= maxCompletions {
				return results
			}
		}
	}
	return results
}

// updateMentionPreview checks if the cursor is inside an @mention and
// populates the candidate list for live display. Called after every keystroke.
func (m *ChatModel) updateMentionPreview() {
	text := m.textarea.Value()
	cursorPos := textareaCursorPos(text, m.textarea.Line(), m.textarea.LineInfo().ColumnOffset)

	start, partial, found := findMentionAtCursor(text, cursorPos)
	if !found {
		m.mention = mentionCompletion{}
		return
	}

	// Only re-glob if the prefix changed
	if m.mention.active && m.mention.prefix == partial {
		return
	}

	candidates := globCompletions(partial)
	if len(candidates) == 0 {
		m.mention = mentionCompletion{}
		return
	}

	m.mention = mentionCompletion{
		active:     true,
		prefix:     partial,
		candidates: candidates,
		index:      0,
		startPos:   start,
	}
}

// handleMentionTab processes a Tab keypress for @-mention completion.
// Returns true if the tab was consumed (mention completion is active).
func (m *ChatModel) handleMentionTab(forward bool) bool {
	if !m.mention.active || len(m.mention.candidates) == 0 {
		return false
	}

	if forward {
		m.mention.index = (m.mention.index + 1) % len(m.mention.candidates)
	} else {
		m.mention.index = (m.mention.index - 1 + len(m.mention.candidates)) % len(m.mention.candidates)
	}
	return true
}

// handleMentionSelect applies the currently selected candidate to the textarea.
// Called on Enter when completion is active (selects without sending).
func (m *ChatModel) handleMentionSelect() {
	if !m.mention.active || len(m.mention.candidates) == 0 {
		return
	}
	m.applyCompletion()
	m.mention = mentionCompletion{}
}

// applyCompletion replaces the @partial with @candidate in the textarea.
func (m *ChatModel) applyCompletion() {
	text := m.textarea.Value()
	candidate := m.mention.candidates[m.mention.index]

	// Build new text: everything before @ + @candidate + everything after current mention
	before := text[:m.mention.startPos]

	// Find the end of the current mention text (from startPos)
	cursorPos := textareaCursorPos(text, m.textarea.Line(), m.textarea.LineInfo().ColumnOffset)
	after := text[cursorPos:]

	newText := before + "@" + candidate + after
	m.textarea.SetValue(newText)

	// SetValue places cursor at end; for the common case (mention at end) this is fine.
	// If there's text after, we accept this tradeoff for v1.
}

// processAtMentions finds all @path tokens in the text, reads each file,
// and returns the cleaned text, file contexts, and any errors.
func (m *ChatModel) processAtMentions(text string) (string, []fileContext, []string) {
	fields := strings.Fields(text)
	var contexts []fileContext
	var errors []string
	seen := make(map[string]bool)

	for _, field := range fields {
		if !strings.HasPrefix(field, "@") {
			continue
		}
		// Check it's a real mention: field must start with @ and have content after
		path := strings.TrimPrefix(field, "@")
		if path == "" {
			continue
		}
		// Ensure the @ was preceded by whitespace or is at start — since we split on
		// fields, each field starting with @ is valid (not user@email.com which is one field)

		// Deduplicate
		if seen[path] {
			continue
		}
		seen[path] = true

		fc, err := readFileForMention(path)
		if err != nil {
			errors = append(errors, fmt.Sprintf("@%s: %s", path, err.Error()))
			continue
		}
		contexts = append(contexts, *fc)
	}

	return text, contexts, errors
}

// readFileForMention reads a file for @-mention injection.
// Enforces: must exist, within cwd, not a directory, not binary, not gitignored, not too large.
func readFileForMention(path string) (*fileContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot determine working directory")
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}
	absPath = filepath.Clean(absPath)

	// Must be within cwd
	if !strings.HasPrefix(absPath, cwd+string(filepath.Separator)) && absPath != cwd {
		return nil, fmt.Errorf("path is outside the project directory")
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found")
		}
		return nil, fmt.Errorf("cannot access file: %v", err)
	}

	if fi.IsDir() {
		return nil, fmt.Errorf("is a directory, not a file")
	}

	if fi.Size() > maxMentionFileSize {
		return nil, fmt.Errorf("file too large (%d KB, max %d KB)", fi.Size()/1024, maxMentionFileSize/1024)
	}

	// Check gitignore
	ctx := context.Background()
	if tools.IsGitIgnored(ctx, absPath) {
		return nil, fmt.Errorf("file is ignored by .gitignore")
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %v", err)
	}

	// Check for binary content
	if bytes.ContainsRune(data, 0) {
		return nil, fmt.Errorf("file appears to be binary")
	}

	content := string(data)
	lines := strings.Count(content, "\n")
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		lines++ // count last line without trailing newline
	}

	return &fileContext{
		path:    path,
		content: content,
		lines:   lines,
	}, nil
}

// renderCompletionStatus returns a status bar string showing the completion candidates.
func (m *ChatModel) renderCompletionStatus() string {
	if !m.mention.active || len(m.mention.candidates) == 0 {
		return ""
	}

	total := len(m.mention.candidates)
	idx := m.mention.index

	// Show a window of candidates around the current selection
	windowSize := 5
	start := idx - windowSize/2
	if start < 0 {
		start = 0
	}
	end := start + windowSize
	if end > total {
		end = total
		start = end - windowSize
		if start < 0 {
			start = 0
		}
	}

	var parts []string
	for i := start; i < end; i++ {
		name := m.mention.candidates[i]
		if i == idx {
			parts = append(parts, MentionSelectedStyle.Render(name))
		} else {
			parts = append(parts, HelpStyle.Render(name))
		}
	}

	hint := fmt.Sprintf("[%d/%d] tab:next shift+tab:prev esc:cancel", idx+1, total)
	return strings.Join(parts, HelpStyle.Render(" | ")) + "  " + HelpStyle.Render(hint)
}
