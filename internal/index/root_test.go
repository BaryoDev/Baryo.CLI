package index

import (
	"os"
	"path/filepath"
	"testing"
)

// The walk skips directories whose name starts with a dot, and the first entry
// WalkDir visits is the root itself. So a project in a dotted directory, say
// ~/.dotfiles or ~/.config/nvim, had its entire tree skipped and the repo map
// came back empty with no error. index.New is given os.Getwd(), so the root's
// own name is whatever the user's directory is called.
func TestDiscoverFilesWalksADottedRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, ".dotfiles")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"main.go", "pkg/helper.go"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := DiscoverFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("found %d files in a dotted root, want 2: %v", len(files), files)
	}
}

// A dotted directory inside the tree is still skipped.
func TestDiscoverFilesSkipsNestedDottedDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden", "secret.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := DiscoverFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Errorf("got %v, want just [main.go]", files)
	}
}
