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

func init() {
	Register("shell", Tool{
		Destructive: true,
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "shell",
				Description: "Run a shell command and return its output. Use for CLI tools like brew, npm, pip, cargo, aws, kubectl, docker, etc.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "The shell command to execute (e.g. \"ls -la\", \"brew install ripgrep\")",
						},
						"working_dir": map[string]interface{}{
							"type":        "string",
							"description": "Working directory to run the command in (defaults to current directory)",
						},
					},
					"required": []string{"command"},
				},
			},
		},
		Execute: executeShell,
	})
}

type shellArgs struct {
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
}

func executeShell(ctx context.Context, argsJSON string) Result {
	var args shellArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if strings.TrimSpace(args.Command) == "" {
		return Result{Content: "command is required", IsError: true}
	}

	workDir, _ := os.Getwd()
	if args.WorkingDir != "" {
		absDir, err := filepath.Abs(args.WorkingDir)
		if err == nil {
			if info, err := os.Stat(absDir); err == nil && info.IsDir() {
				workDir = absDir
			}
		}
	}

	cmdArgs := []string{"sh", "-c", args.Command}
	return runCommand(ctx, cmdArgs, workDir)
}
