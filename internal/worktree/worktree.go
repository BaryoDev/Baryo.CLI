// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package worktree

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Worktree represents an isolated git worktree.
type Worktree struct {
	Name       string
	Path       string
	Branch     string
	BaseCommit string
}

// Create creates a new git worktree with a unique branch name.
// The worktree is placed in .baryo/worktrees/<name>.
func Create(name string) (*Worktree, error) {
	if name == "" {
		name = fmt.Sprintf("baryo-%d", time.Now().Unix())
	}

	// Get current commit hash
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	baseCommit := strings.TrimSpace(string(out))

	// Get repo root
	rootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("cannot find git root: %w", err)
	}
	root := strings.TrimSpace(string(rootOut))

	branch := "baryo/" + name
	path := root + "/.baryo/worktrees/" + name

	// Create worktree with new branch
	cmd := exec.Command("git", "worktree", "add", "-b", branch, path)
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
	if out, err := exec.Command("git", "worktree", "remove", w.Path, "--force").CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove failed: %s", strings.TrimSpace(string(out)))
	}
	// Delete the branch
	exec.Command("git", "branch", "-D", w.Branch).Run()
	return nil
}

// HasChanges returns true if the worktree has uncommitted changes.
func (w *Worktree) HasChanges() bool {
	cmd := exec.Command("git", "-C", w.Path, "diff", "--quiet")
	if err := cmd.Run(); err != nil {
		return true
	}
	// Also check staged changes
	cmd = exec.Command("git", "-C", w.Path, "diff", "--cached", "--quiet")
	return cmd.Run() != nil
}
