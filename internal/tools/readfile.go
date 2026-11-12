// SPDX-License-Identifier: MIT

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/ignore"
)

const maxFileSize = 100 * 1024 // 100 KB

// maxReadBytes is the largest file read_file and grep will load into memory.
const maxReadBytes = 10 * 1024 * 1024

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

	// Ensure the path is within the working directory, including after
	// symlink resolution.
	absPath, err := resolveWithinProject(cwd, args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}

	// Open the resolved path, not the name that was checked.
	realPath, err := resolveReadable(cwd, args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}

	// Check .baryoignore + .gitignore on the name and on where it really
	// points, so a link to .env is not a way around the ignore rules.
	if ignore.IsIgnored(ctx, absPath) || (realPath != absPath && ignore.IsIgnored(ctx, realPath)) {
		return Result{Content: fmt.Sprintf("file is ignored: %s", args.Path), IsError: true}
	}

	// Refuse very large files before reading them into memory.
	if fi, err := os.Stat(realPath); err == nil && fi.Size() > maxReadBytes {
		return Result{Content: fmt.Sprintf("file is too large to read (%d bytes, limit %d): %s", fi.Size(), maxReadBytes, args.Path), IsError: true}
	}

	// Read the file.
	data, err := os.ReadFile(realPath)
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
