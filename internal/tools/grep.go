// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	maxContextLines = 5
	defaultFileGlob = "**/*"
)

func init() {
	Register("grep", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "grep",
				Description: "Search file contents by regex pattern. Returns matching lines with file paths and line numbers (like ripgrep). Skips gitignored and binary files.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"pattern": map[string]interface{}{
							"type":        "string",
							"description": "Regex pattern to search for (Go regexp syntax).",
						},
						"glob": map[string]interface{}{
							"type":        "string",
							"description": "Optional file filter glob pattern (e.g. *.go, **/*.ts). Defaults to **/* (all files).",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Optional subdirectory to search in, relative to the project root. Defaults to project root.",
						},
						"context_lines": map[string]interface{}{
							"type":        "integer",
							"description": "Number of context lines to show before and after each match (0-5, default 0).",
						},
					},
					"required": []string{"pattern"},
				},
			},
		},
		Execute: executeGrep,
	})
}

type grepArgs struct {
	Pattern      string `json:"pattern"`
	Glob         string `json:"glob,omitempty"`
	Path         string `json:"path,omitempty"`
	ContextLines *int   `json:"context_lines,omitempty"`
}

func executeGrep(ctx context.Context, argsJSON string) Result {
	var args grepArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Pattern == "" {
		return Result{Content: "pattern is required", IsError: true}
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid regex pattern: %v", err), IsError: true}
	}

	ctxLines := 0
	if args.ContextLines != nil {
		ctxLines = *args.ContextLines
		if ctxLines < 0 {
			ctxLines = 0
		}
		if ctxLines > maxContextLines {
			ctxLines = maxContextLines
		}
	}

	fileGlob := defaultFileGlob
	if args.Glob != "" {
		fileGlob = args.Glob
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

	info, err := os.Stat(searchRoot)
	if err != nil || !info.IsDir() {
		return Result{Content: fmt.Sprintf("directory not found: %s", args.Path), IsError: true}
	}

	files, err := doublestar.Glob(os.DirFS(searchRoot), fileGlob)
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid glob pattern: %v", err), IsError: true}
	}

	var b strings.Builder
	totalMatches := 0
	filesWithMatches := 0
	outputSize := 0

	for _, f := range files {
		absPath := filepath.Join(searchRoot, f)

		fi, err := os.Stat(absPath)
		if err != nil || fi.IsDir() {
			continue
		}

		if IsGitIgnored(ctx, absPath) {
			continue
		}

		fileMatches := searchFile(absPath, cwd, re, ctxLines, &b, &outputSize)
		if fileMatches > 0 {
			totalMatches += fileMatches
			filesWithMatches++
		}

		if outputSize >= maxFileSize {
			fmt.Fprintf(&b, "\n... (output truncated at %dKB)\n", maxFileSize/1024)
			break
		}
	}

	if totalMatches == 0 {
		return Result{Content: "no matches found"}
	}

	fmt.Fprintf(&b, "\n%d matches in %d files", totalMatches, filesWithMatches)

	return Result{Content: b.String()}
}

// searchFile searches a single file for regex matches and writes results to b.
// Returns the number of matches found.
func searchFile(absPath, cwd string, re *regexp.Regexp, ctxLines int, b *strings.Builder, outputSize *int) int {
	// Read file and check for binary content.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return 0
	}

	// Skip binary files (check first 512 bytes for null bytes).
	peek := data
	if len(peek) > 512 {
		peek = peek[:512]
	}
	if bytes.ContainsRune(peek, 0) {
		return 0
	}

	rel, err := filepath.Rel(cwd, absPath)
	if err != nil {
		rel = absPath
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	matchCount := 0
	printed := make(map[int]bool) // track lines already printed to avoid duplicates with context

	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		matchCount++

		// Determine context range.
		start := i - ctxLines
		if start < 0 {
			start = 0
		}
		end := i + ctxLines + 1
		if end > len(lines) {
			end = len(lines)
		}

		// Add separator between context groups if needed.
		if matchCount > 1 && ctxLines > 0 && !printed[start-1] {
			line := "--\n"
			b.WriteString(line)
			*outputSize += len(line)
		}

		for j := start; j < end; j++ {
			if printed[j] {
				continue
			}
			printed[j] = true

			var out string
			if j == i {
				out = fmt.Sprintf("%s:%d: %s\n", rel, j+1, lines[j])
			} else {
				out = fmt.Sprintf("%s:%d- %s\n", rel, j+1, lines[j])
			}
			b.WriteString(out)
			*outputSize += len(out)

			if *outputSize >= maxFileSize {
				return matchCount
			}
		}
	}

	return matchCount
}
