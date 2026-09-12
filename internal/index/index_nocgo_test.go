package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No build tag on purpose. The tree-sitter parsers are cgo-only, and the
// CGO_ENABLED=0 parser stub returned an error, which Build treats as
// "unparseable" and skips. So every file was dropped and released binaries,
// which goreleaser builds with CGO_ENABLED=0, shipped an empty repo map.
//
// Symbols are allowed to be absent without cgo. Files are not.
func TestIndexKeepsFilesWithoutSymbolExtraction(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"main.go":       "package main\n\nfunc main() {}\n",
		"pkg/helper.go": "package pkg\n\nfunc Helper() {}\n",
		"web/app.js":    "function hello() {}\n",
		"notes.md":      "# not a source file\n",
	} {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	idx := New(root)
	if err := idx.Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := idx.FileCount(); got != 3 {
		t.Errorf("FileCount = %d, want 3 source files indexed", got)
	}

	repoMap := idx.RepoMap(2000)
	if repoMap == "" {
		t.Fatal("RepoMap is empty: the model gets no view of the project at all")
	}
	for _, want := range []string{"main.go", "helper.go", "app.js"} {
		if !strings.Contains(repoMap, want) {
			t.Errorf("RepoMap does not mention %s:\n%s", want, repoMap)
		}
	}
}

// SymbolsAvailable tells the rest of the program, and the user via doctor,
// whether this build can extract symbols at all. Under cgo the index must
// actually produce some, so a broken parser cannot hide behind an empty map.
func TestSymbolsPresentWhenAvailable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc Alpha() {}\n\nfunc Beta() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := New(root)
	if err := idx.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	fs := idx.FileSymbolsFor("main.go")
	if fs == nil {
		t.Fatal("main.go is not in the index")
	}

	if SymbolsAvailable {
		if len(fs.Symbols) == 0 {
			t.Error("this build extracts symbols, but none were found in main.go")
		}
		if !strings.Contains(idx.RepoMap(2000), "Alpha") {
			t.Errorf("RepoMap omits a symbol it should have:\n%s", idx.RepoMap(2000))
		}
		return
	}
	if len(fs.Symbols) != 0 {
		t.Errorf("this build cannot extract symbols, got %d", len(fs.Symbols))
	}
}
