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
				Description: "Make an exact text replacement in a file. The old_string must appear exactly once in the file.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Relative path to the file from the project root.",
						},
						"old_string": map[string]interface{}{
							"type":        "string",
							"description": "The exact text to find and replace. Must appear exactly once in the file.",
						},
						"new_string": map[string]interface{}{
							"type":        "string",
							"description": "The replacement text.",
						},
					},
					"required": []string{"path", "old_string", "new_string"},
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

func executeEditFile(ctx context.Context, argsJSON string) Result {
	var args editFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}
	}
	if args.OldString == "" {
		return Result{Content: "old_string is required", IsError: true}
	}
	if args.OldString == args.NewString {
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

	// Count occurrences of old_string.
	count := strings.Count(content, args.OldString)
	if count == 0 {
		return Result{Content: "old_string not found in file", IsError: true}
	}
	if count > 1 {
		return Result{Content: fmt.Sprintf("old_string appears %d times, provide more context to make it unique", count), IsError: true}
	}

	// Perform the replacement.
	newContent := strings.Replace(content, args.OldString, args.NewString, 1)

	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("cannot write file: %v", err), IsError: true}
	}

	return Result{Content: fmt.Sprintf("Edited %s: replaced %d chars with %d chars", args.Path, len(args.OldString), len(args.NewString))}
}
