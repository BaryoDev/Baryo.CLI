// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/arnelirobles/baryo-cli/internal/ignore"
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
	startPos   int      // byte position of @ in the text
	pending    string   // prefix currently being globbed (empty if none in flight)
}

// fileContext holds the content of an @-mentioned file.
type fileContext struct {
	path    string
	content string
	lines   int
}

// textareaCursorPos returns the absolute byte position of the cursor in the
// textarea value. It takes the line number and the rune offset within that line
// (from LineInfo().CharOffset) and converts to a byte position.
func textareaCursorPos(value string, line int, charOffset int) int {
	lines := strings.Split(value, "\n")
	pos := 0
	for i := 0; i < line && i < len(lines); i++ {
		pos += len(lines[i]) + 1 // +1 for newline
	}
	// Convert rune offset to byte offset within the current line
	if line < len(lines) {
		runes := []rune(lines[line])
		if charOffset > len(runes) {
			charOffset = len(runes)
		}
		pos += len(string(runes[:charOffset]))
	}
	return pos
}

// findMentionAtCursor scans backward from cursor to find an @ preceded by
// whitespace or start-of-string. Returns the start byte position, the partial
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

// batchGitIgnored checks multiple paths against .gitignore in a single
// git subprocess call. Returns a set of ignored paths.
func batchGitIgnored(paths []string) map[string]bool {
	ignored := make(map[string]bool)
	if len(paths) == 0 {
		return ignored
	}

	cmd := exec.Command("git", "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n"))
	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 = none ignored, other = git not available (allow all)
		return ignored
	}

	// -z produces null-separated output
	for _, p := range bytes.Split(out, []byte{0}) {
		s := string(p)
		if s != "" {
			ignored[s] = true
		}
	}
	return ignored
}

// globCompletions returns file paths matching the partial prefix.
// Directories get a trailing /, .git/ and gitignored files are filtered out.
// Uses a single batched git check-ignore call instead of per-file subprocess.
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

	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	// Collect all glob matches first
	seen := make(map[string]bool)
	type matchInfo struct {
		relPath string
		absPath string
		isDir   bool
	}
	var allMatches []matchInfo

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
			fi, err := os.Stat(absPath)
			if err != nil {
				continue
			}

			allMatches = append(allMatches, matchInfo{
				relPath: m,
				absPath: absPath,
				isDir:   fi.IsDir(),
			})
		}
	}

	// Batch gitignore check — one subprocess for all paths
	absPaths := make([]string, len(allMatches))
	for i, m := range allMatches {
		absPaths[i] = m.absPath
	}
	ignored := batchGitIgnored(absPaths)

	// Build results, filtering ignored
	var results []string
	for _, m := range allMatches {
		if ignored[m.absPath] {
			continue
		}
		if m.isDir {
			results = append(results, m.relPath+"/")
		} else {
			results = append(results, m.relPath)
		}
		if len(results) >= maxCompletions {
			break
		}
	}
	return results
}

// updateMentionPreview checks if the cursor is inside an @mention and
// kicks off async globbing if the prefix changed. Returns a tea.Cmd if
// globbing needs to run, nil otherwise.
func (m *ChatModel) updateMentionPreview() tea.Cmd {
	text := m.textarea.Value()
	cursorPos := textareaCursorPos(text, m.textarea.Line(), m.textarea.LineInfo().CharOffset)

	start, partial, found := findMentionAtCursor(text, cursorPos)
	if !found {
		m.mention = mentionCompletion{}
		return nil
	}

	// Skip if prefix hasn't changed (already showing or already pending)
	if m.mention.active && m.mention.prefix == partial {
		return nil
	}
	if m.mention.pending == partial {
		return nil
	}

	// Mark as pending and launch async glob
	m.mention.pending = partial
	capturedStart := start
	capturedPartial := partial
	return func() tea.Msg {
		candidates := globCompletions(capturedPartial)
		return MentionCandidatesMsg{
			Prefix:     capturedPartial,
			StartPos:   capturedStart,
			Candidates: candidates,
		}
	}
}

// handleMentionCandidates processes the result of an async glob.
func (m *ChatModel) handleMentionCandidates(msg MentionCandidatesMsg) {
	m.mention.pending = ""

	// Stale result — user typed more since we started globbing
	text := m.textarea.Value()
	cursorPos := textareaCursorPos(text, m.textarea.Line(), m.textarea.LineInfo().CharOffset)
	_, currentPartial, found := findMentionAtCursor(text, cursorPos)
	if !found || currentPartial != msg.Prefix {
		return
	}

	if len(msg.Candidates) == 0 {
		m.mention = mentionCompletion{}
		return
	}

	m.mention = mentionCompletion{
		active:     true,
		prefix:     msg.Prefix,
		candidates: msg.Candidates,
		index:      0,
		startPos:   msg.StartPos,
	}
}

// handleMentionTab processes a Tab keypress for @-mention completion.
func (m *ChatModel) handleMentionTab(forward bool) {
	if !m.mention.active || len(m.mention.candidates) == 0 {
		return
	}

	if forward {
		m.mention.index = (m.mention.index + 1) % len(m.mention.candidates)
	} else {
		m.mention.index = (m.mention.index - 1 + len(m.mention.candidates)) % len(m.mention.candidates)
	}
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
// Appends a trailing space after file completions (not directories) to prevent
// the live preview from immediately re-triggering.
func (m *ChatModel) applyCompletion() {
	text := m.textarea.Value()
	candidate := m.mention.candidates[m.mention.index]

	// Build new text: everything before @ + @candidate + everything after current mention
	before := text[:m.mention.startPos]

	// Find the end of the current mention text (from startPos)
	cursorPos := textareaCursorPos(text, m.textarea.Line(), m.textarea.LineInfo().CharOffset)
	after := text[cursorPos:]

	// Append space after file selections so the preview doesn't re-trigger
	suffix := ""
	if !strings.HasSuffix(candidate, "/") {
		suffix = " "
	}

	newText := before + "@" + candidate + suffix + after
	m.textarea.SetValue(newText)
}

// processAtMentions finds all @path tokens in the text, reads each file,
// and returns the original text, file contexts, and any errors.
func (m *ChatModel) processAtMentions(text string) (string, []fileContext, []string) {
	fields := strings.Fields(text)
	var contexts []fileContext
	var errors []string
	seen := make(map[string]bool)

	for _, field := range fields {
		if !strings.HasPrefix(field, "@") {
			continue
		}
		path := strings.TrimPrefix(field, "@")
		if path == "" {
			continue
		}

		// Strip trailing punctuation (e.g. @main.go, or @main.go.)
		path = strings.TrimRight(path, ".,;:!?)")

		if path == "" {
			continue
		}

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
// Enforces: must exist, within cwd, not a directory, not binary, not gitignored,
// not too large, and resolves symlinks to prevent escaping the project directory.
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

	// Resolve symlinks to prevent escaping cwd via symlink
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found")
		}
		return nil, fmt.Errorf("cannot access file: %v", err)
	}

	// Also resolve cwd symlinks for a correct prefix check
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, fmt.Errorf("cannot determine working directory")
	}

	// Must be within cwd (after symlink resolution)
	if !strings.HasPrefix(resolved, resolvedCwd+string(filepath.Separator)) && resolved != resolvedCwd {
		return nil, fmt.Errorf("path is outside the project directory")
	}

	fi, err := os.Stat(resolved)
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

	// Check .baryoignore + .gitignore
	ctx := context.Background()
	if ignore.IsIgnored(ctx, resolved) {
		return nil, fmt.Errorf("file is ignored")
	}

	data, err := os.ReadFile(resolved)
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

	// Ensure valid UTF-8 (extra safety check)
	if !utf8.ValidString(content) {
		return nil, fmt.Errorf("file appears to be binary")
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
