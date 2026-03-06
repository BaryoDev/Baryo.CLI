// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/ignore"
)

func init() {
	Register("apply_diff", Tool{
		Destructive: true,
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "apply_diff",
				Description: "Apply a unified diff to a file. Use this for multi-hunk edits where edit_file would require many calls. The diff should use standard unified diff format with @@ hunk headers.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Relative path to the file from the project root.",
						},
						"diff": map[string]interface{}{
							"type":        "string",
							"description": "Unified diff content. Each hunk starts with @@ -start,count +start,count @@. Lines beginning with - are removed, + are added, and space (or no prefix) are context.",
						},
					},
					"required": []string{"path", "diff"},
				},
			},
		},
		Execute: executeApplyDiff,
	})
}

type applyDiffArgs struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

// diffHunk represents a single hunk from a unified diff.
type diffHunk struct {
	oldStart int
	oldCount int
	lines    []diffLine
}

type diffLine struct {
	op   byte   // ' ', '+', '-'
	text string // line content (without the op prefix)
}

func executeApplyDiff(ctx context.Context, argsJSON string) Result {
	var args applyDiffArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}
	}
	if args.Diff == "" {
		return Result{Content: "diff is required", IsError: true}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Result{Content: fmt.Sprintf("cannot determine working directory: %v", err), IsError: true}
	}

	absPath := args.Path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}
	absPath = filepath.Clean(absPath)

	if !strings.HasPrefix(absPath, cwd+string(filepath.Separator)) && absPath != cwd {
		return Result{Content: "path is outside the project directory", IsError: true}
	}

	if ignore.IsIgnored(ctx, absPath) {
		return Result{Content: fmt.Sprintf("file is ignored: %s", args.Path), IsError: true}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Content: fmt.Sprintf("file not found: %s", args.Path), IsError: true}
		}
		return Result{Content: fmt.Sprintf("cannot read file: %v", err), IsError: true}
	}

	if bytes.ContainsRune(data, 0) {
		return Result{Content: fmt.Sprintf("file appears to be binary: %s", args.Path), IsError: true}
	}

	hunks, err := parseUnifiedDiff(args.Diff)
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid diff: %v", err), IsError: true}
	}
	if len(hunks) == 0 {
		return Result{Content: "no hunks found in diff", IsError: true}
	}

	lines := strings.Split(string(data), "\n")
	result, err := applyHunks(lines, hunks)
	if err != nil {
		return Result{Content: fmt.Sprintf("failed to apply diff: %v", err), IsError: true}
	}

	newContent := strings.Join(result, "\n")
	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("cannot write file: %v", err), IsError: true}
	}

	return Result{Content: fmt.Sprintf("Applied %d hunk(s) to %s", len(hunks), args.Path)}
}

// parseUnifiedDiff parses a unified diff string into hunks.
func parseUnifiedDiff(diff string) ([]diffHunk, error) {
	var hunks []diffHunk
	lines := strings.Split(diff, "\n")

	var current *diffHunk
	for _, line := range lines {
		// Skip file-level headers.
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}

		if strings.HasPrefix(line, "@@") {
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, h)
			current = &hunks[len(hunks)-1]
			continue
		}

		if current == nil {
			continue // skip lines before first hunk
		}

		if len(line) == 0 {
			// Empty line = context line with empty content.
			current.lines = append(current.lines, diffLine{op: ' ', text: ""})
			continue
		}

		switch line[0] {
		case '+':
			current.lines = append(current.lines, diffLine{op: '+', text: line[1:]})
		case '-':
			current.lines = append(current.lines, diffLine{op: '-', text: line[1:]})
		case ' ':
			current.lines = append(current.lines, diffLine{op: ' ', text: line[1:]})
		default:
			// Treat as context line (some diffs omit the leading space).
			current.lines = append(current.lines, diffLine{op: ' ', text: line})
		}
	}

	return hunks, nil
}

// parseHunkHeader parses a @@ -start,count +start,count @@ line.
func parseHunkHeader(line string) (diffHunk, error) {
	// Strip @@ markers and any trailing section heading.
	line = strings.TrimPrefix(line, "@@")
	if idx := strings.Index(line[1:], "@@"); idx >= 0 {
		line = line[:idx+1]
	}
	line = strings.TrimSpace(line)

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return diffHunk{}, fmt.Errorf("malformed hunk header")
	}

	old := strings.TrimPrefix(parts[0], "-")
	oldStart, oldCount, err := parseRange(old)
	if err != nil {
		return diffHunk{}, fmt.Errorf("malformed hunk header old range: %v", err)
	}

	return diffHunk{oldStart: oldStart, oldCount: oldCount}, nil
}

// parseRange parses "start,count" or "start" into integers.
func parseRange(s string) (start, count int, err error) {
	if idx := strings.Index(s, ","); idx >= 0 {
		start, err = strconv.Atoi(s[:idx])
		if err != nil {
			return
		}
		count, err = strconv.Atoi(s[idx+1:])
		return
	}
	start, err = strconv.Atoi(s)
	count = 1
	return
}

// applyHunks applies parsed hunks to file lines (1-indexed).
func applyHunks(lines []string, hunks []diffHunk) ([]string, error) {
	// Apply hunks in reverse order to preserve line numbers.
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		var err error
		lines, err = applyHunk(lines, h)
		if err != nil {
			return nil, fmt.Errorf("hunk %d: %v", i+1, err)
		}
	}
	return lines, nil
}

// applyHunk applies a single hunk to lines.
func applyHunk(lines []string, h diffHunk) ([]string, error) {
	// hunk line numbers are 1-indexed.
	pos := h.oldStart - 1
	if pos < 0 {
		pos = 0
	}

	// Verify context lines match and build the replacement.
	var newLines []string
	fileIdx := pos
	for _, dl := range h.lines {
		switch dl.op {
		case ' ':
			if fileIdx >= len(lines) {
				return nil, fmt.Errorf("context line %d beyond end of file", fileIdx+1)
			}
			if strings.TrimRight(lines[fileIdx], " \t\r") != strings.TrimRight(dl.text, " \t\r") {
				return nil, fmt.Errorf("context mismatch at line %d: expected %q, got %q", fileIdx+1, dl.text, lines[fileIdx])
			}
			newLines = append(newLines, lines[fileIdx])
			fileIdx++
		case '-':
			if fileIdx >= len(lines) {
				return nil, fmt.Errorf("remove line %d beyond end of file", fileIdx+1)
			}
			// Verify the line matches what we expect to remove.
			if strings.TrimRight(lines[fileIdx], " \t\r") != strings.TrimRight(dl.text, " \t\r") {
				return nil, fmt.Errorf("remove mismatch at line %d: expected %q, got %q", fileIdx+1, dl.text, lines[fileIdx])
			}
			fileIdx++ // skip this line (remove it)
		case '+':
			newLines = append(newLines, dl.text)
		}
	}

	// Splice: replace lines[pos:fileIdx] with newLines.
	result := make([]string, 0, len(lines)-fileIdx+pos+len(newLines))
	result = append(result, lines[:pos]...)
	result = append(result, newLines...)
	result = append(result, lines[fileIdx:]...)
	return result, nil
}
