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

	"github.com/arnelirobles/baryo-cli/internal/fsutil"
	"github.com/arnelirobles/baryo-cli/internal/ignore"
)

const maxWriteSize = 500 * 1024 // 500 KB

func init() {
	Register("write_file", Tool{
		Destructive: true,
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "write_file",
				Description: "Create or overwrite a file with the given content. Creates parent directories if needed.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Relative path to the file from the project root.",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "The full content to write to the file.",
						},
					},
					"required": []string{"path", "content"},
				},
			},
		},
		Execute: executeWriteFile,
	})
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func executeWriteFile(ctx context.Context, argsJSON string) Result {
	var args writeFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}
	}

	if len(args.Content) > maxWriteSize {
		return Result{Content: fmt.Sprintf("content too large: %d bytes (max %d)", len(args.Content), maxWriteSize), IsError: true}
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

	// Create parent directories if needed.
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{Content: fmt.Sprintf("cannot create directories: %v", err), IsError: true}
	}

	// Write the file.
	if err := fsutil.WriteFileAtomic(absPath, []byte(args.Content), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("cannot write file: %v", err), IsError: true}
	}

	return Result{Content: fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), args.Path)}
}
