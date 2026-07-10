// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package worktree

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds each git invocation so a blocked git (e.g. waiting on an
// index lock) cannot hang the app.
const gitTimeout = 30 * time.Second

// Worktree represents an isolated git worktree.
type Worktree struct {
	Name       string
	Path       string
	Branch     string
	BaseCommit string
}

func gitCommand(args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	return exec.CommandContext(ctx, "git", args...), cancel
}

// Create creates a new git worktree with a unique branch name.
// The worktree is placed in .baryo/worktrees/<name>.
func Create(name string) (*Worktree, error) {
	if name == "" {
		name = fmt.Sprintf("baryo-%d", time.Now().Unix())
	}

	// Get current commit hash
	cmd, cancel := gitCommand("rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	cancel()
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	baseCommit := strings.TrimSpace(string(out))

	// Get repo root
	cmd, cancel = gitCommand("rev-parse", "--show-toplevel")
	rootOut, err := cmd.Output()
	cancel()
	if err != nil {
		return nil, fmt.Errorf("cannot find git root: %w", err)
	}
	root := strings.TrimSpace(string(rootOut))

	branch := "baryo/" + name
	path := root + "/.baryo/worktrees/" + name

	// Create worktree with new branch
	cmd, cancel = gitCommand("worktree", "add", "-b", branch, path)
	defer cancel()
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add failed: %s", strings.TrimSpace(string(out)))
	}

	return &Worktree{
		Name:       name,
		Path:       path,
		Branch:     branch,
		BaseCommit: baseCommit,
	}, nil
}

// Remove removes the worktree and deletes the branch.
func (w *Worktree) Remove() error {
	// Remove the worktree
	cmd, cancel := gitCommand("worktree", "remove", w.Path, "--force")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %s", strings.TrimSpace(string(out)))
	}
	// Delete the branch
	cmd, cancel = gitCommand("branch", "-D", w.Branch)
	defer cancel()
	cmd.Run()
	return nil
}

// HasChanges returns true if the worktree has uncommitted changes.
// A failed git invocation also reports true — conservative, so callers never
// discard a worktree based on a broken check.
func (w *Worktree) HasChanges() bool {
	cmd, cancel := gitCommand("-C", w.Path, "diff", "--quiet")
	err := cmd.Run()
	cancel()
	if err != nil {
		return true
	}
	// Also check staged changes
	cmd, cancel = gitCommand("-C", w.Path, "diff", "--cached", "--quiet")
	defer cancel()
	return cmd.Run() != nil
}
