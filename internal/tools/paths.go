// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/ignore"
)

// resolveWithinProject resolves path against cwd and verifies it stays inside
// the project directory, including after symlink resolution — a symlink inside
// the project must not redirect file operations outside it. Returns the
// cleaned absolute path.
func resolveWithinProject(cwd, path string) (string, error) {
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}
	absPath = filepath.Clean(absPath)
	abs, _, err := resolveReal(cwd, absPath)
	return abs, err
}

// resolveReadable is resolveWithinProject for read tools. It returns the fully
// resolved real path, which the caller must open, so a link swapped in after
// the check cannot redirect the read.
func resolveReadable(cwd, path string) (real string, err error) {
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}
	_, real, err = resolveReal(cwd, filepath.Clean(absPath))
	return real, err
}

func resolveReal(cwd, absPath string) (abs, real string, err error) {
	if !strings.HasPrefix(absPath, cwd+string(filepath.Separator)) && absPath != cwd {
		return "", "", fmt.Errorf("path is outside the project directory")
	}

	resolved, err := resolveExistingPrefix(absPath)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve path: %v", err)
	}
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolvedCwd = cwd
	}
	if !strings.HasPrefix(resolved, resolvedCwd+string(filepath.Separator)) && resolved != resolvedCwd {
		return "", "", fmt.Errorf("path resolves outside the project directory")
	}
	return absPath, resolved, nil
}

// resolveExistingPrefix evaluates symlinks on the deepest existing ancestor of
// p and rejoins the non-existing remainder, so paths about to be created can
// still be validated.
func resolveExistingPrefix(p string) (string, error) {
	suffix := ""
	cur := p
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		suffix = filepath.Join(filepath.Base(cur), suffix)
		cur = parent
	}
}

// existingMode returns the permission bits of the file at path so a rewrite
// keeps them (an executable script stays executable, a 0600 file stays 0600).
// A path that does not exist yet gets 0644.
func existingMode(path string) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	return 0o644
}

// realPathIgnored applies the ignore rules to where a path really points. The
// ignore package works on paths under cwd, so the real path is mapped back into
// cwd's own spelling first (cwd may itself sit behind a symlink). A real path
// that cannot be mapped inside the project counts as ignored. lexical is the
// name that was already checked, which is not checked twice.
func realPathIgnored(ctx context.Context, cwd, lexical, realPath string) bool {
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolvedCwd = cwd
	}
	rel, err := filepath.Rel(resolvedCwd, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	mapped := filepath.Join(cwd, rel)
	if mapped == lexical {
		return false
	}
	return ignore.IsIgnored(ctx, mapped)
}
