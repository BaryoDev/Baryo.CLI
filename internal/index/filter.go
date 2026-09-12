// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/ignore"
)

// extToLang maps file extensions to tree-sitter language names.
var extToLang = map[string]string{
	".go":   "go",
	".js":   "javascript",
	".jsx":  "javascript",
	".ts":   "typescript",
	".tsx":  "typescript",
	".py":   "python",
	".rs":   "rust",
	".java": "java",
	".c":    "c",
	".h":    "c",
	".cpp":  "cpp",
	".cc":   "cpp",
	".cxx":  "cpp",
	".hpp":  "cpp",
}

// skipDirs are directories that should always be skipped during discovery.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".venv":        true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"bin":          true,
	".next":        true,
}

// maxFileSize is the maximum file size to parse (1MB).
const maxFileSize = 1 << 20

// LangForFile returns the tree-sitter language name for a file extension,
// or empty string if the file type is not supported.
func LangForFile(path string) string {
	return extToLang[strings.ToLower(filepath.Ext(path))]
}

// DiscoverFiles walks the project root and returns relative paths of parseable
// source files. It skips ignored directories, binary files, and files over 1MB.
func DiscoverFiles(root string) ([]string, error) {
	ctx := context.Background()
	var candidates []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		name := d.Name()

		// Skip hidden dirs and known non-source dirs. Never the root itself:
		// WalkDir visits it first, and a project in a dotted directory such as
		// ~/.dotfiles would otherwise have its whole tree skipped.
		if d.IsDir() {
			if path == root {
				return nil
			}
			if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process files with known extensions.
		if LangForFile(name) == "" {
			return nil
		}

		// Check file size.
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > maxFileSize || info.Size() == 0 {
			return nil
		}

		candidates = append(candidates, path)
		return nil
	})

	// One batched ignore check for the whole tree. IsIgnored forks a git
	// subprocess per path, so this walk used to cost one process per file, and
	// it runs after every completed turn. Filtering before isBinary also avoids
	// opening files that are excluded anyway.
	ignored := ignore.Filter(ctx, candidates)

	files := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if ignored[path] || isBinary(path) {
			continue
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			continue
		}
		files = append(files, rel)
	}

	return files, err
}

// isBinary checks if a file contains null bytes in the first 512 bytes.
func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}
