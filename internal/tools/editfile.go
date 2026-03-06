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
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/ignore"
)

func init() {
	Register("edit_file", Tool{
		Destructive: true,
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "edit_file",
				Description: "Make a text replacement in a file. The old_string should appear exactly once. If old_string is empty and the file is under 100 lines, the entire file is replaced with new_string (whole-file rewrite). Whitespace differences are tolerated via fuzzy matching if an exact match fails.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Relative path to the file from the project root.",
						},
						"old_string": map[string]interface{}{
							"type":        "string",
							"description": "The text to find and replace. Must appear exactly once. Leave empty to rewrite the entire file (only for files under 100 lines).",
						},
						"new_string": map[string]interface{}{
							"type":        "string",
							"description": "The replacement text.",
						},
					},
					"required": []string{"path", "new_string"},
				},
			},
		},
		Execute: executeEditFile,
	})
}

type editFileArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// maxWholeFileRewriteLines is the maximum file size (in lines) for whole-file rewrite mode.
const maxWholeFileRewriteLines = 100

func executeEditFile(ctx context.Context, argsJSON string) Result {
	var args editFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}
	}
	if args.OldString == args.NewString && args.OldString != "" {
		return Result{Content: "old_string and new_string are identical", IsError: true}
	}

	// Resolve to absolute path based on cwd.
	cwd, err := os.Getwd()
	if err != nil {
		return Result{Content: fmt.Sprintf("cannot determine working directory: %v", err), IsError: true}
	}

	absPath := args.Path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}
	absPath = filepath.Clean(absPath)

	// Ensure the path is within the working directory.
	if !strings.HasPrefix(absPath, cwd+string(filepath.Separator)) && absPath != cwd {
		return Result{Content: "path is outside the project directory", IsError: true}
	}

	// Check .baryoignore + .gitignore.
	if ignore.IsIgnored(ctx, absPath) {
		return Result{Content: fmt.Sprintf("file is ignored: %s", args.Path), IsError: true}
	}

	// Whole-file rewrite mode: old_string is empty.
	if args.OldString == "" {
		data, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Create new file.
				if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
					return Result{Content: fmt.Sprintf("cannot create directory: %v", err), IsError: true}
				}
				if err := os.WriteFile(absPath, []byte(args.NewString), 0o644); err != nil {
					return Result{Content: fmt.Sprintf("cannot write file: %v", err), IsError: true}
				}
				return Result{Content: fmt.Sprintf("Created %s (%d chars)", args.Path, len(args.NewString))}
			}
			return Result{Content: fmt.Sprintf("cannot read file: %v", err), IsError: true}
		}
		// File exists — check line count.
		lineCount := strings.Count(string(data), "\n") + 1
		if lineCount > maxWholeFileRewriteLines {
			return Result{Content: fmt.Sprintf("file has %d lines (max %d for whole-file rewrite) — use old_string to target a specific section", lineCount, maxWholeFileRewriteLines), IsError: true}
		}
		if err := os.WriteFile(absPath, []byte(args.NewString), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("cannot write file: %v", err), IsError: true}
		}
		return Result{Content: fmt.Sprintf("Rewrote %s (%d lines)", args.Path, lineCount)}
	}

	// Read the file.
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Content: fmt.Sprintf("file not found: %s", args.Path), IsError: true}
		}
		return Result{Content: fmt.Sprintf("cannot read file: %v", err), IsError: true}
	}

	// Reject binary files.
	if bytes.ContainsRune(data, 0) {
		return Result{Content: fmt.Sprintf("file appears to be binary: %s", args.Path), IsError: true}
	}

	content := string(data)

	// Count occurrences of old_string (exact match).
	count := strings.Count(content, args.OldString)
	if count == 1 {
		// Exact match found.
		newContent := strings.Replace(content, args.OldString, args.NewString, 1)
		if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("cannot write file: %v", err), IsError: true}
		}
		return Result{Content: fmt.Sprintf("Edited %s: replaced %d chars with %d chars", args.Path, len(args.OldString), len(args.NewString))}
	}
	if count > 1 {
		return Result{Content: fmt.Sprintf("old_string appears %d times, provide more context to make it unique", count), IsError: true}
	}

	// Exact match failed (count == 0) — try fuzzy matching.
	match, found := fuzzyFind(content, args.OldString)
	if !found {
		return Result{Content: "old_string not found in file (exact and fuzzy match both failed)", IsError: true}
	}

	newContent := strings.Replace(content, match, args.NewString, 1)
	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("cannot write file: %v", err), IsError: true}
	}
	return Result{Content: fmt.Sprintf("Edited %s (fuzzy matched): replaced %d chars with %d chars", args.Path, len(match), len(args.NewString))}
}

// normalizeWhitespace collapses runs of whitespace and trims each line.
func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Collapse all whitespace runs to single space, trim.
		fields := strings.Fields(line)
		lines[i] = strings.Join(fields, " ")
	}
	return strings.Join(lines, "\n")
}

// fuzzyFind searches content for a unique match of oldString using
// normalized whitespace comparison. Returns the original (non-normalized)
// matching text and whether a unique match was found.
func fuzzyFind(content, oldString string) (exactMatch string, found bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(oldString, "\n")

	if len(oldLines) == 0 || len(contentLines) < len(oldLines) {
		return "", false
	}

	// Normalize the search target.
	normOld := make([]string, len(oldLines))
	for i, line := range oldLines {
		normOld[i] = strings.Join(strings.Fields(line), " ")
	}

	var matches []int // start line indices of matches
	windowSize := len(oldLines)

	for i := 0; i <= len(contentLines)-windowSize; i++ {
		match := true
		for j := range windowSize {
			normContent := strings.Join(strings.Fields(contentLines[i+j]), " ")
			if normContent != normOld[j] {
				match = false
				break
			}
		}
		if match {
			matches = append(matches, i)
		}
	}

	if len(matches) != 1 {
		return "", false // no match or ambiguous
	}

	// Extract the original text from the content.
	startIdx := matches[0]
	originalLines := contentLines[startIdx : startIdx+windowSize]
	return strings.Join(originalLines, "\n"), true
}
