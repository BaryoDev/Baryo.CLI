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
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/procutil"
)

const maxGhOutput = 100 * 1024 // 100 KB

// allowedGhSubcommands maps top-level gh commands to their allowed subcommands.
var allowedGhSubcommands = map[string]map[string]bool{
	"pr":      {"list": true, "view": true, "diff": true, "checks": true, "status": true},
	"issue":   {"list": true, "view": true, "status": true},
	"release": {"list": true, "view": true},
	"repo":    {"view": true},
	"run":     {"list": true, "view": true},
	"api":     {}, // handled separately by validateGhApi
}

func init() {
	Register("gh", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "gh",
				Description: "Run a read-only GitHub CLI command. Supports: pr (list/view/diff/checks/status), issue (list/view/status), release (list/view), repo (view), run (list/view), and api (GET only).",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"args": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Arguments to pass to the gh CLI (e.g. [\"pr\", \"list\"]).",
						},
					},
					"required": []string{"args"},
				},
			},
		},
		Execute: executeGh,
	})
}

type ghArgs struct {
	Args []string `json:"args"`
}

func executeGh(_ context.Context, argsJSON string) Result {
	var args ghArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if len(args.Args) == 0 {
		return Result{Content: "args is required and must not be empty", IsError: true}
	}

	// Check that gh is installed.
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return Result{
			Content: "gh CLI is not installed. Install it from https://cli.github.com/",
			IsError: true,
		}
	}

	// Validate the command against the allowlist.
	if err := validateGhCommand(args.Args); err != nil {
		return Result{Content: err.Error(), IsError: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return Result{Content: fmt.Sprintf("cannot determine working directory: %v", err), IsError: true}
	}

	cmd := exec.CommandContext(ctx, ghPath, args.Args...)
	cmd.Dir = cwd

	var buf procutil.CappedBuffer
	buf.Max = maxGhOutput
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(buf.String())
		if msg == "" {
			msg = err.Error()
		}
		return Result{Content: msg, IsError: true}
	}

	content := buf.String()
	if buf.Truncated() {
		content += "\n... (truncated)"
	}

	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}

	return Result{Content: content}
}

// validateGhCommand checks that the gh arguments are in the read-only allowlist.
func validateGhCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}

	resource := args[0]
	subcommands, ok := allowedGhSubcommands[resource]
	if !ok {
		allowed := make([]string, 0, len(allowedGhSubcommands))
		for k := range allowedGhSubcommands {
			allowed = append(allowed, k)
		}
		return fmt.Errorf("command %q is not allowed; allowed commands: %s", resource, strings.Join(allowed, ", "))
	}

	// Special handling for "api".
	if resource == "api" {
		return validateGhApi(args[1:])
	}

	if len(args) < 2 {
		return fmt.Errorf("missing subcommand for %q", resource)
	}

	action := args[1]
	if !subcommands[action] {
		allowed := make([]string, 0, len(subcommands))
		for k := range subcommands {
			allowed = append(allowed, k)
		}
		return fmt.Errorf("action %q is not allowed for %q; allowed: %s", action, resource, strings.Join(allowed, ", "))
	}

	return nil
}

// validateGhApi ensures gh api calls are GET-only.
// Blocks -X with non-GET methods, and -f/-F flags (which imply POST) unless -X GET is explicit.
func validateGhApi(args []string) error {
	hasExplicitGet := false
	hasMutationFlag := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Check for -X / --method flag.
		if arg == "-X" || arg == "--method" {
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for %s flag", arg)
			}
			method := strings.ToUpper(args[i+1])
			if method != "GET" {
				return fmt.Errorf("only GET requests are allowed for gh api; got %s", method)
			}
			hasExplicitGet = true
			i++ // skip the method value
			continue
		}

		// Check for combined form like -XGET or -XPOST.
		if strings.HasPrefix(arg, "-X") && len(arg) > 2 {
			method := strings.ToUpper(arg[2:])
			if method != "GET" {
				return fmt.Errorf("only GET requests are allowed for gh api; got %s", method)
			}
			hasExplicitGet = true
			continue
		}

		// Check for field flags that imply POST.
		if arg == "-f" || arg == "-F" || arg == "--field" || arg == "--raw-field" {
			hasMutationFlag = true
		}
	}

	// If -f/-F is used without explicit -X GET, block it (implies POST).
	if hasMutationFlag && !hasExplicitGet {
		return fmt.Errorf("-f/-F flags imply POST; add -X GET if you intend a GET request")
	}

	return nil
}
