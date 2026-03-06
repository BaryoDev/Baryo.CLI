package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLangForFile(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.js", "javascript"},
		{"App.jsx", "javascript"},
		{"index.ts", "typescript"},
		{"Component.tsx", "typescript"},
		{"script.py", "python"},
		{"README.md", ""},
		{"data.json", ""},
		{"style.css", ""},
		{"image.png", ""},
		{"", ""},
		{"Makefile", ""},
		{".go", "go"}, // just extension
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := LangForFile(tt.path)
			if got != tt.want {
				t.Errorf("LangForFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestLangForFile_CaseInsensitive(t *testing.T) {
	// Extension matching should be case-insensitive
	tests := []struct {
		path string
		want string
	}{
		{"file.GO", "go"},
		{"file.Py", "python"},
		{"file.JS", "javascript"},
		{"file.TS", "typescript"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := LangForFile(tt.path)
			if got != tt.want {
				t.Errorf("LangForFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	dir := t.TempDir()

	// Create a text file
	textPath := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(textPath, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	if isBinary(textPath) {
		t.Error("text file should not be detected as binary")
	}

	// Create a binary file (has null bytes)
	binPath := filepath.Join(dir, "binary.bin")
	data := make([]byte, 100)
	data[50] = 0 // null byte
	for i := range 50 {
		data[i] = byte('a' + (i % 26))
	}
	if err := os.WriteFile(binPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	if !isBinary(binPath) {
		t.Error("file with null bytes should be detected as binary")
	}

	// Nonexistent file should return true (safe default)
	if !isBinary(filepath.Join(dir, "nonexistent")) {
		t.Error("nonexistent file should be treated as binary")
	}
}

func TestSkipDirs(t *testing.T) {
	expected := []string{
		".git", "node_modules", "vendor", "__pycache__",
		".venv", "dist", "build", "target", "bin", ".next",
	}
	for _, dir := range expected {
		if !skipDirs[dir] {
			t.Errorf("skipDirs should contain %q", dir)
		}
	}
}

func TestMaxFileSize(t *testing.T) {
	if maxFileSize != 1<<20 {
		t.Errorf("maxFileSize = %d, want %d (1MB)", maxFileSize, 1<<20)
	}
}

func TestDiscoverFiles(t *testing.T) {
	dir := t.TempDir()

	// Create Go and Python files
	goDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(goDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "utils.py"), []byte("def hello(): pass"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a non-parseable file (should be excluded)
	if err := os.WriteFile(filepath.Join(goDir, "readme.md"), []byte("# Readme"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a file in a skip directory (should be excluded)
	nodeModules := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(nodeModules, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeModules, "index.js"), []byte("module.exports = {}"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := DiscoverFiles(dir)
	if err != nil {
		t.Fatalf("DiscoverFiles error: %v", err)
	}

	// Should find main.go and utils.py, but not readme.md or node_modules/pkg/index.js
	found := make(map[string]bool)
	for _, f := range files {
		found[f] = true
	}

	if !found[filepath.Join("src", "main.go")] {
		t.Error("should discover src/main.go")
	}
	if !found[filepath.Join("src", "utils.py")] {
		t.Error("should discover src/utils.py")
	}
	if found[filepath.Join("src", "readme.md")] {
		t.Error("should not discover readme.md")
	}
	if found[filepath.Join("node_modules", "pkg", "index.js")] {
		t.Error("should not discover files in node_modules")
	}
}

func TestDiscoverFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	files, err := DiscoverFiles(dir)
	if err != nil {
		t.Fatalf("DiscoverFiles error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("empty dir should return no files, got %d", len(files))
	}
}
