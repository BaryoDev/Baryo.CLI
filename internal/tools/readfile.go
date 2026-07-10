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

const maxFileSize = 100 * 1024 // 100 KB

func init() {
	Register("read_file", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "read_file",
				Description: "Read the contents of a file in the project. Returns the file content with line numbers. Respects .gitignore and rejects binary files.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Relative path to the file from the project root.",
						},
						"start_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional 1-based start line number.",
						},
						"end_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional 1-based end line number (inclusive).",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		Execute: executeReadFile,
	})
}

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine *int   `json:"start_line,omitempty"`
	EndLine   *int   `json:"end_line,omitempty"`
}

func executeReadFile(ctx context.Context, argsJSON string) Result {
	var args readFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}
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

	// Read the file.
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Content: fmt.Sprintf("file not found: %s", args.Path), IsError: true}
		}
		return Result{Content: fmt.Sprintf("cannot read file: %v", err), IsError: true}
	}

	// Check for binary content (null bytes).
	if bytes.ContainsRune(data, 0) {
		return Result{Content: fmt.Sprintf("file appears to be binary: %s", args.Path), IsError: true}
	}

	lines := strings.Split(string(data), "\n")

	// Apply line range if specified.
	startIdx := 0
	endIdx := len(lines)
	lineOffset := 1 // for display numbering

	if args.StartLine != nil {
		if *args.StartLine < 1 {
			return Result{Content: "start_line must be >= 1", IsError: true}
		}
		startIdx = *args.StartLine - 1
		lineOffset = *args.StartLine
		if startIdx >= len(lines) {
			return Result{Content: fmt.Sprintf("start_line %d exceeds file length (%d lines)", *args.StartLine, len(lines)), IsError: true}
		}
	}

	if args.EndLine != nil {
		if *args.EndLine < 1 {
			return Result{Content: "end_line must be >= 1", IsError: true}
		}
		endIdx = *args.EndLine
		if endIdx > len(lines) {
			endIdx = len(lines)
		}
	}

	if startIdx >= endIdx {
		return Result{Content: "start_line must be less than end_line", IsError: true}
	}

	selected := lines[startIdx:endIdx]

	// Format with line numbers.
	var b strings.Builder
	for i, line := range selected {
		fmt.Fprintf(&b, "%4d | %s\n", lineOffset+i, line)
	}

	content := b.String()

	// Truncate if too large.
	if len(content) > maxFileSize {
		content = content[:maxFileSize] + "\n... (truncated)"
	}

	return Result{Content: content}
}
