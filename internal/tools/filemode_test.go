// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"testing"
)

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestEditFileKeepsMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o755, 0o600} {
		symlinkProject(t)
		if err := os.WriteFile("run.sh", []byte("echo one\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod("run.sh", mode); err != nil {
			t.Fatal(err)
		}
		res := executeEditFile(context.Background(), argsJSON(t, map[string]any{
			"path": "run.sh", "old_string": "one", "new_string": "two",
		}))
		if res.IsError {
			t.Fatalf("edit failed: %s", res.Content)
		}
		if got := modeOf(t, "run.sh"); got != mode {
			t.Errorf("edit_file changed mode %v to %v", mode, got)
		}
	}
}

func TestWriteFileKeepsModeAndNewFileDefault(t *testing.T) {
	symlinkProject(t)
	if err := os.WriteFile("run.sh", []byte("echo one\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod("run.sh", 0o755); err != nil {
		t.Fatal(err)
	}
	res := executeWriteFile(context.Background(), argsJSON(t, map[string]any{"path": "run.sh", "content": "echo two\n"}))
	if res.IsError {
		t.Fatalf("write failed: %s", res.Content)
	}
	if got := modeOf(t, "run.sh"); got != 0o755 {
		t.Errorf("write_file changed mode 0755 to %v", got)
	}
	res = executeWriteFile(context.Background(), argsJSON(t, map[string]any{"path": "fresh.txt", "content": "x\n"}))
	if res.IsError {
		t.Fatalf("write failed: %s", res.Content)
	}
	if got := modeOf(t, "fresh.txt"); got != 0o644 {
		t.Errorf("new file mode = %v, want 0644", got)
	}
}

func TestApplyDiffKeepsMode(t *testing.T) {
	symlinkProject(t)
	if err := os.WriteFile("run.sh", []byte("a\nb\nc\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod("run.sh", 0o755); err != nil {
		t.Fatal(err)
	}
	diff := "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	res := executeApplyDiff(context.Background(), argsJSON(t, map[string]any{"path": "run.sh", "diff": diff}))
	if res.IsError {
		t.Fatalf("apply_diff failed: %s", res.Content)
	}
	if got := modeOf(t, "run.sh"); got != 0o755 {
		t.Errorf("apply_diff changed mode 0755 to %v", got)
	}
}
