// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/arnelirobles/baryo-cli/internal/ignore"
)

func init() {
	Register("delete_file", Tool{
		Destructive: true,
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "delete_file",
				Description: "Delete a file from the project. Only deletes single files, not directories.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Relative path to the file from the project root.",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		Execute: executeDeleteFile,
	})
}

type deleteFileArgs struct {
	Path string `json:"path"`
}

func executeDeleteFile(ctx context.Context, argsJSON string) Result {
	var args deleteFileArgs
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

	absPath, err := resolveWithinProject(cwd, args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}
	}

	// Check .baryoignore + .gitignore.
	if ignore.IsIgnored(ctx, absPath) {
		return Result{Content: fmt.Sprintf("file is ignored: %s", args.Path), IsError: true}
	}

	// Check that it exists and is a file (not a directory).
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Content: fmt.Sprintf("file not found: %s", args.Path), IsError: true}
		}
		return Result{Content: fmt.Sprintf("cannot access file: %v", err), IsError: true}
	}
	if info.IsDir() {
		return Result{Content: fmt.Sprintf("path is a directory, not a file: %s", args.Path), IsError: true}
	}

	// Delete the file.
	if err := os.Remove(absPath); err != nil {
		return Result{Content: fmt.Sprintf("cannot delete file: %v", err), IsError: true}
	}

	return Result{Content: fmt.Sprintf("Deleted %s", args.Path)}
}
