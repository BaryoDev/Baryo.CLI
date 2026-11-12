// SPDX-License-Identifier: MIT

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

// ghApiValueFlags are the gh api flags that take a value, by long and short
// name. ghApiBoolFlags take none. Any other flag is treated as a write (or as
// unknown) and refused, so a new gh flag is blocked until it is reviewed.
var (
	ghApiValueFlags = map[string]string{
		"--method": "method", "-X": "method",
		"--jq": "plain", "-q": "plain",
		"--template": "plain", "-t": "plain",
		"--hostname": "plain", "--cache": "plain",
		"--preview": "plain", "-p": "plain",
		"--header": "header", "-H": "header",
		"--field": "field", "-F": "typedfield",
		"--raw-field": "field", "-f": "field",
	}
	ghApiBoolFlags = map[string]bool{
		"--paginate": true, "--slurp": true, "--silent": true,
		"--include": true, "-i": true, "--verbose": true,
	}
)

// splitGhFlag returns the flag name and its inline value for the forms
// "--name=value", "-Xvalue" and a bare "-X". hasValue is false for a bare flag.
func splitGhFlag(arg string) (name, value string, hasValue bool) {
	if strings.HasPrefix(arg, "--") {
		if i := strings.Index(arg, "="); i >= 0 {
			return arg[:i], arg[i+1:], true
		}
		return arg, "", false
	}
	if len(arg) > 2 {
		return arg[:2], strings.TrimPrefix(arg[2:], "="), true
	}
	return arg, "", false
}

// validateGhApi ensures gh api calls are read-only. It allows an explicit list
// of flags and refuses everything else. Field flags (which make gh send a
// POST) are allowed only with an explicit GET method, where gh turns them into
// query parameters. --input, graphql, and header method overrides are refused.
func validateGhApi(args []string) error {
	hasExplicitGet := false
	hasField := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if strings.EqualFold(strings.Trim(arg, "/"), "graphql") {
				return fmt.Errorf("gh api graphql is not allowed; it is always a POST")
			}
			continue
		}

		if ghApiBoolFlags[arg] {
			continue
		}

		name, value, hasValue := splitGhFlag(arg)
		kind, ok := ghApiValueFlags[name]
		if !ok {
			return fmt.Errorf("flag %q is not allowed for gh api (read-only flags only)", arg)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for %s flag", name)
			}
			i++
			value = args[i]
		}

		switch kind {
		case "method":
			if strings.ToUpper(value) != "GET" {
				return fmt.Errorf("only GET requests are allowed for gh api; got %s", strings.ToUpper(value))
			}
			hasExplicitGet = true
		case "header":
			if strings.Contains(strings.ToLower(value), "method-override") {
				return fmt.Errorf("method override headers are not allowed for gh api")
			}
		case "typedfield":
			if strings.Contains(value, "=@") {
				return fmt.Errorf("-F values that read a file are not allowed for gh api")
			}
			hasField = true
		case "field":
			hasField = true
		}
	}

	if hasField && !hasExplicitGet {
		return fmt.Errorf("-f/-F flags imply POST; add -X GET if you intend a GET request")
	}

	return nil
}
