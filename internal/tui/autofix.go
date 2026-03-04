// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// AutoFixConfig controls post-edit lint/test behavior.
type AutoFixConfig struct {
	AutoLint    bool
	AutoTest    bool
	LintCommand string
	TestCommand string
}

// codeModifyingTools is the set of tools that modify project files.
var codeModifyingTools = map[string]bool{
	"edit_file":   true,
	"write_file":  true,
	"delete_file": true,
}

// isCodeModifyingTool returns true for tools that modify project source files.
func isCodeModifyingTool(name string) bool {
	return codeModifyingTools[name]
}

// detectLinter returns a command and args based on project markers.
func detectLinter() (string, []string) {
	if _, err := os.Stat("go.mod"); err == nil {
		if _, err := exec.LookPath("golangci-lint"); err == nil {
			return "golangci-lint", []string{"run"}
		}
		return "go", []string{"vet", "./..."}
	}
	if _, err := os.Stat("package.json"); err == nil {
		return "npx", []string{"eslint", "."}
	}
	if _, err := os.Stat("Cargo.toml"); err == nil {
		return "cargo", []string{"clippy"}
	}
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return "python", []string{"-m", "flake8"}
	}
	return "", nil
}

// detectAutoTestRunner returns a command and args for running tests in auto-fix mode.
func detectAutoTestRunner() (string, []string) {
	if _, err := os.Stat("go.mod"); err == nil {
		return "go", []string{"test", "-short", "./..."}
	}
	if _, err := os.Stat("package.json"); err == nil {
		return "npx", []string{"jest", "--bail"}
	}
	if _, err := os.Stat("Cargo.toml"); err == nil {
		return "cargo", []string{"test"}
	}
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return "python", []string{"-m", "pytest", "--tb=short", "-q"}
	}
	return "", nil
}

const (
	autoCheckTimeout  = 30 * time.Second
	maxAutoCheckChars = 4000
)

// runAutoCheck runs linter and/or tests after a code-modifying tool call.
// Returns empty string if all pass; returns error output on failure.
func runAutoCheck(ctx context.Context, cfg AutoFixConfig) string {
	var parts []string

	if cfg.AutoLint {
		if out := runOneCheck(ctx, cfg.LintCommand, detectLinter, "auto-lint"); out != "" {
			parts = append(parts, out)
		}
	}

	if cfg.AutoTest {
		if out := runOneCheck(ctx, cfg.TestCommand, detectAutoTestRunner, "auto-test"); out != "" {
			parts = append(parts, out)
		}
	}

	result := strings.Join(parts, "\n\n")
	if len(result) > maxAutoCheckChars {
		result = result[:maxAutoCheckChars] + "\n... (truncated)"
	}
	return result
}

// runOneCheck executes a single check command. If customCmd is set, it is used
// (split by spaces); otherwise detectFn provides the command and args.
// Returns formatted error output, or empty string on success.
func runOneCheck(ctx context.Context, customCmd string, detectFn func() (string, []string), label string) string {
	var cmd string
	var args []string

	if customCmd != "" {
		parts := strings.Fields(customCmd)
		if len(parts) == 0 {
			return ""
		}
		cmd = parts[0]
		args = parts[1:]
	} else {
		cmd, args = detectFn()
		if cmd == "" {
			return ""
		}
	}

	checkCtx, cancel := context.WithTimeout(ctx, autoCheckTimeout)
	defer cancel()

	var buf bytes.Buffer
	c := exec.CommandContext(checkCtx, cmd, args...)
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		output := strings.TrimSpace(buf.String())
		if output == "" {
			output = err.Error()
		}
		return "[" + label + " errors]\n" + output
	}
	return ""
}
