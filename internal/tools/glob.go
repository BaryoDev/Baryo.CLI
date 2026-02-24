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
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const maxGlobResults = 200

func init() {
	Register("glob", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "glob",
				Description: "Find files matching a glob pattern. Supports ** for recursive matching (e.g. **/*.go). Returns matching file paths sorted alphabetically. Respects .gitignore.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"pattern": map[string]interface{}{
							"type":        "string",
							"description": "Glob pattern to match files (e.g. **/*.go, src/**/*.ts, *.md).",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Optional subdirectory to search in, relative to the project root. Defaults to project root.",
						},
					},
					"required": []string{"pattern"},
				},
			},
		},
		Execute: executeGlob,
	})
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func executeGlob(ctx context.Context, argsJSON string) Result {
	var args globArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Pattern == "" {
		return Result{Content: "pattern is required", IsError: true}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Result{Content: fmt.Sprintf("cannot determine working directory: %v", err), IsError: true}
	}

	searchRoot := cwd
	if args.Path != "" {
		searchRoot = args.Path
		if !filepath.IsAbs(searchRoot) {
			searchRoot = filepath.Join(cwd, searchRoot)
		}
		searchRoot = filepath.Clean(searchRoot)

		if !strings.HasPrefix(searchRoot, cwd+string(filepath.Separator)) && searchRoot != cwd {
			return Result{Content: "path is outside the project directory", IsError: true}
		}
	}

	// Verify search root exists.
	info, err := os.Stat(searchRoot)
	if err != nil || !info.IsDir() {
		return Result{Content: fmt.Sprintf("directory not found: %s", args.Path), IsError: true}
	}

	matches, err := doublestar.Glob(os.DirFS(searchRoot), args.Pattern)
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid glob pattern: %v", err), IsError: true}
	}

	// Filter out gitignored files, directories, and .git internals.
	var filtered []string
	for _, m := range matches {
		// Skip .git directory contents.
		if m == ".git" || strings.HasPrefix(m, ".git/") || strings.HasPrefix(m, ".git"+string(filepath.Separator)) {
			continue
		}

		absPath := filepath.Join(searchRoot, m)

		// Skip directories — only return files.
		fi, err := os.Stat(absPath)
		if err != nil || fi.IsDir() {
			continue
		}

		if IsGitIgnored(ctx, absPath) {
			continue
		}
		// Return paths relative to cwd.
		rel, err := filepath.Rel(cwd, absPath)
		if err != nil {
			rel = m
		}
		filtered = append(filtered, rel)
	}

	sort.Strings(filtered)

	total := len(filtered)
	if total == 0 {
		return Result{Content: "no files matched"}
	}

	truncated := false
	if total > maxGlobResults {
		filtered = filtered[:maxGlobResults]
		truncated = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) matched\n\n", total)
	for _, f := range filtered {
		b.WriteString(f)
		b.WriteByte('\n')
	}
	if truncated {
		fmt.Fprintf(&b, "\n(truncated — %d more files)", total-maxGlobResults)
	}

	return Result{Content: b.String()}
}
