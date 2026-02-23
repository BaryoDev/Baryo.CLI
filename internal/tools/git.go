// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxGitOutput = 100 * 1024 // 100 KB

func init() {
	Register("git_status", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "git_status",
				Description: "Show the current git status including modified, staged, and untracked files with branch info.",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		Execute: executeGitStatus,
	})

	Register("git_diff", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "git_diff",
				Description: "Show file diffs. Use staged=true for staged changes, or pass file paths to limit the diff.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"staged": map[string]interface{}{
							"type":        "boolean",
							"description": "If true, show only staged (--cached) changes.",
						},
						"files": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Optional list of relative file paths to limit the diff to.",
						},
					},
				},
			},
		},
		Execute: executeGitDiff,
	})

	Register("git_log", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "git_log",
				Description: "Show recent commit history. Defaults to the last 10 commits.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"count": map[string]interface{}{
							"type":        "integer",
							"description": "Number of commits to show (default 10, max 50).",
						},
						"file": map[string]interface{}{
							"type":        "string",
							"description": "Optional relative file path to show history for.",
						},
					},
				},
			},
		},
		Execute: executeGitLog,
	})
}

// runGit executes a git command with a 10-second timeout and returns the output.
func runGit(ctx context.Context, args ...string) Result {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return Result{Content: fmt.Sprintf("cannot determine working directory: %v", err), IsError: true}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Return git errors (e.g. "not a git repo") as tool errors.
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return Result{Content: msg, IsError: true}
	}

	content := string(out)
	if len(content) > maxGitOutput {
		content = content[:maxGitOutput] + "\n... (truncated)"
	}

	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}

	return Result{Content: content}
}

// validateRelativePath checks that a path is relative, has no ".." segments,
// and stays within the project directory.
func validateRelativePath(path string) error {
	if filepath.IsAbs(path) {
		return fmt.Errorf("path must be relative: %s", path)
	}
	cleaned := filepath.Clean(path)
	for _, part := range strings.Split(cleaned, string(filepath.Separator)) {
		if part == ".." {
			return fmt.Errorf("path must not contain '..': %s", path)
		}
	}
	return nil
}

func executeGitStatus(_ context.Context, argsJSON string) Result {
	ctx := context.Background()
	return runGit(ctx, "status", "--short", "--branch")
}

type gitDiffArgs struct {
	Staged bool     `json:"staged,omitempty"`
	Files  []string `json:"files,omitempty"`
}

func executeGitDiff(_ context.Context, argsJSON string) Result {
	var args gitDiffArgs
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
		}
	}

	gitArgs := []string{"diff"}
	if args.Staged {
		gitArgs = append(gitArgs, "--cached")
	}

	// Validate and append file paths after --.
	if len(args.Files) > 0 {
		for _, f := range args.Files {
			if err := validateRelativePath(f); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
		}
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, args.Files...)
	}

	ctx := context.Background()
	return runGit(ctx, gitArgs...)
}

type gitLogArgs struct {
	Count *int   `json:"count,omitempty"`
	File  string `json:"file,omitempty"`
}

func executeGitLog(_ context.Context, argsJSON string) Result {
	var args gitLogArgs
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
		}
	}

	count := 10
	if args.Count != nil {
		count = *args.Count
		if count < 1 {
			count = 1
		}
		if count > 50 {
			count = 50
		}
	}

	gitArgs := []string{"log", "--oneline", fmt.Sprintf("-n%d", count)}

	if args.File != "" {
		if err := validateRelativePath(args.File); err != nil {
			return Result{Content: err.Error(), IsError: true}
		}
		gitArgs = append(gitArgs, "--", args.File)
	}

	ctx := context.Background()
	return runGit(ctx, gitArgs...)
}
