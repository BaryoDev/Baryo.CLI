// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxListEntries = 500

func init() {
	Register("list_directory", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "list_directory",
				Description: "List directory contents as a tree. Returns an indented listing of files and subdirectories. Respects .gitignore. Use when the user asks about project structure or what files exist.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Directory to list, relative to project root. Use \".\" for current directory. Defaults to \".\" if omitted.",
						},
						"depth": map[string]interface{}{
							"type":        "integer",
							"description": "Max recursion depth (1-10, default 3).",
						},
					},
				},
			},
		},
		Execute: executeListDir,
	})
}

type listDirArgs struct {
	Path  string `json:"path,omitempty"`
	Depth *int   `json:"depth,omitempty"`
}

func executeListDir(ctx context.Context, argsJSON string) Result {
	var args listDirArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Result{Content: fmt.Sprintf("cannot determine working directory: %v", err), IsError: true}
	}

	root := cwd
	if args.Path != "" && args.Path != "." {
		root = args.Path
		if !filepath.IsAbs(root) {
			root = filepath.Join(cwd, root)
		}
		root = filepath.Clean(root)

		if !strings.HasPrefix(root, cwd+string(filepath.Separator)) && root != cwd {
			return Result{Content: "path is outside the project directory", IsError: true}
		}
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		// Fall back to project root for unrecognized paths.
		root = cwd
		info, err = os.Stat(root)
		if err != nil || !info.IsDir() {
			return Result{Content: fmt.Sprintf("directory not found: %s", args.Path), IsError: true}
		}
	}

	maxDepth := 3
	if args.Depth != nil {
		maxDepth = *args.Depth
	}
	if maxDepth < 1 {
		maxDepth = 1
	}
	if maxDepth > 10 {
		maxDepth = 10
	}

	var b strings.Builder
	count := 0
	overflow := 0
	walkDir(ctx, root, "", 0, maxDepth, &b, &count, &overflow)

	if overflow > 0 {
		fmt.Fprintf(&b, "\n(truncated — %d more entries)", overflow)
	}

	out := b.String()
	if out == "" {
		return Result{Content: "(empty directory)"}
	}
	return Result{Content: fmt.Sprintf("%d entries\n\n%s", count+overflow, out)}
}

// walkDir recursively lists directory contents with indentation.
func walkDir(ctx context.Context, dir, indent string, depth, maxDepth int, b *strings.Builder, count, overflow *int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if *count >= maxListEntries {
			*overflow++
			continue
		}

		// Skip .git directory.
		if e.Name() == ".git" {
			continue
		}

		absPath := filepath.Join(dir, e.Name())

		if isGitIgnored(ctx, absPath) {
			continue
		}

		*count++
		if e.IsDir() {
			fmt.Fprintf(b, "%s%s/\n", indent, e.Name())
			if depth+1 < maxDepth {
				walkDir(ctx, absPath, indent+"  ", depth+1, maxDepth, b, count, overflow)
			}
		} else {
			fmt.Fprintf(b, "%s%s\n", indent, e.Name())
		}
	}
}
