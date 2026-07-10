// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	if !strings.HasPrefix(absPath, cwd+string(filepath.Separator)) && absPath != cwd {
		return "", fmt.Errorf("path is outside the project directory")
	}

	resolved, err := resolveExistingPrefix(absPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path: %v", err)
	}
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolvedCwd = cwd
	}
	if !strings.HasPrefix(resolved, resolvedCwd+string(filepath.Separator)) && resolved != resolvedCwd {
		return "", fmt.Errorf("path resolves outside the project directory")
	}
	return absPath, nil
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
