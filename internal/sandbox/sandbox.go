// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/procutil"
)

const (
	execTimeout = 30 * time.Second
	maxOutput   = 200 * 1024 // 200 KB
)

// Sandbox manages sandboxed code execution via Docker containers.
type Sandbox struct {
	Available bool
}

// New checks if Docker is available and returns a Sandbox.
func New() *Sandbox {
	err := exec.Command("docker", "info").Run()
	return &Sandbox{Available: err == nil}
}

// imageForLang returns the Docker image to use for a given language.
func imageForLang(lang string) (string, string) {
	switch strings.ToLower(lang) {
	case "python":
		return "python:3-slim", "python3"
	case "node", "javascript", "js":
		return "node:20-slim", "node"
	case "shell", "bash", "sh":
		return "alpine:3", "sh"
	case "go", "golang":
		return "golang:1.22-alpine", "go run"
	case "ruby", "rb":
		return "ruby:3-slim", "ruby"
	default:
		return "alpine:3", "sh"
	}
}

// Execute runs code in a Docker container with the working directory mounted read-only.
func (s *Sandbox) Execute(ctx context.Context, lang, code, workDir string) (string, error) {
	if !s.Available {
		return "", fmt.Errorf("docker is not available for sandboxed execution")
	}

	image, interpreter := imageForLang(lang)

	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	args := []string{
		"run", "--rm",
		"--network=none",
		"--memory=256m",
		"--cpus=1",
	}

	// Mount working directory read-only
	if workDir != "" {
		args = append(args, "-v", workDir+":/workspace:ro", "-w", "/workspace")
	}

	args = append(args, image)

	// For Go, we need to write to a temp file and run it
	if strings.ToLower(lang) == "go" || strings.ToLower(lang) == "golang" {
		args = append(args, "sh", "-c",
			fmt.Sprintf("cd /tmp && echo '%s' > main.go && go run main.go",
				strings.ReplaceAll(code, "'", "'\"'\"'")))
	} else {
		args = append(args, interpreter, "-c", code)
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var buf procutil.CappedBuffer
	buf.Max = maxOutput
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	output := buf.String()
	if buf.Truncated() {
		output += "\n... (output truncated)"
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("execution timed out after %s", execTimeout)
		}
		return output, fmt.Errorf("execution failed: %w", err)
	}

	return output, nil
}
