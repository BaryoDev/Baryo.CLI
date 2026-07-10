// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWithinProject(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{project, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := resolveWithinProject(project, "sub/file.txt"); err != nil {
		t.Errorf("relative path inside project rejected: %v", err)
	}

	if _, err := resolveWithinProject(project, "../outside/file.txt"); err == nil {
		t.Error("expected rejection of ../ traversal")
	}

	if _, err := resolveWithinProject(project, outside); err == nil {
		t.Error("expected rejection of absolute path outside project")
	}
}

func TestResolveWithinProjectSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{project, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// A symlink inside the project pointing outside it.
	link := filepath.Join(project, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	if _, err := resolveWithinProject(project, "link/escape.txt"); err == nil {
		t.Error("expected rejection of write through symlink escaping the project")
	}

	// A symlink to a file outside the project.
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(project, "filelink.txt")
	if err := os.Symlink(target, fileLink); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveWithinProject(project, "filelink.txt"); err == nil {
		t.Error("expected rejection of file symlink escaping the project")
	}

	// A symlink that stays inside the project is fine.
	inner := filepath.Join(project, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	innerLink := filepath.Join(project, "innerlink")
	if err := os.Symlink(inner, innerLink); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveWithinProject(project, "innerlink/ok.txt"); err != nil {
		t.Errorf("symlink staying inside project rejected: %v", err)
	}
}
