// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// symlinkProject builds a project with links that lead to a directory and a
// file outside it, plus one real file inside.
func symlinkProject(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{project, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET-OUTSIDE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "real.txt"), []byte("TOPSECRET-INSIDE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(project, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
}

func argsJSON(t *testing.T, v map[string]any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadFileRefusesSymlinkOutsideProject(t *testing.T) {
	symlinkProject(t)
	for _, p := range []string{"link/secret.txt", "notes.txt"} {
		res := executeReadFile(context.Background(), argsJSON(t, map[string]any{"path": p}))
		if !res.IsError || strings.Contains(res.Content, "TOPSECRET-OUTSIDE") {
			t.Errorf("read_file %s must be refused, got %q", p, res.Content)
		}
	}
	res := executeReadFile(context.Background(), argsJSON(t, map[string]any{"path": "real.txt"}))
	if res.IsError || !strings.Contains(res.Content, "TOPSECRET-INSIDE") {
		t.Errorf("a real file must still read, got %q", res.Content)
	}
}

func TestReadFileRefusesHugeFile(t *testing.T) {
	symlinkProject(t)
	f, err := os.Create("big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxReadBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	res := executeReadFile(context.Background(), argsJSON(t, map[string]any{"path": "big.txt"}))
	if !res.IsError || !strings.Contains(res.Content, "too large") {
		t.Errorf("expected a too large error, got %q", res.Content)
	}
}

func TestGrepDoesNotFollowSymlinksOutside(t *testing.T) {
	symlinkProject(t)
	for _, a := range []map[string]any{
		{"pattern": "TOPSECRET"},
		{"pattern": "TOPSECRET", "path": "."},
	} {
		res := executeGrep(context.Background(), argsJSON(t, a))
		if strings.Contains(res.Content, "OUTSIDE") {
			t.Errorf("grep leaked outside content: %q", res.Content)
		}
		if !strings.Contains(res.Content, "real.txt") {
			t.Errorf("grep should still find the real file: %q", res.Content)
		}
	}
	res := executeGrep(context.Background(), argsJSON(t, map[string]any{"pattern": "TOPSECRET", "path": "link"}))
	if !res.IsError {
		t.Errorf("grep with a path through a symlink outside must be refused, got %q", res.Content)
	}
}

func TestGlobDoesNotFollowSymlinksOutside(t *testing.T) {
	symlinkProject(t)
	res := executeGlob(context.Background(), argsJSON(t, map[string]any{"pattern": "**/*.txt"}))
	if strings.Contains(res.Content, "notes.txt") || strings.Contains(res.Content, "secret.txt") {
		t.Errorf("glob listed a file outside the project: %q", res.Content)
	}
	if !strings.Contains(res.Content, "real.txt") {
		t.Errorf("glob should still list the real file: %q", res.Content)
	}
	res = executeGlob(context.Background(), argsJSON(t, map[string]any{"pattern": "*.txt", "path": "link"}))
	if !res.IsError {
		t.Errorf("glob with a path through a symlink outside must be refused, got %q", res.Content)
	}
}
