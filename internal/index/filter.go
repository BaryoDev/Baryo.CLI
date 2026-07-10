// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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
	var files []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		name := d.Name()

		// Skip hidden dirs and known non-source dirs.
		if d.IsDir() {
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

		// Check ignore rules (.baryoignore + .gitignore).
		if ignore.IsIgnored(ctx, path) {
			return nil
		}

		// Check for binary content (null bytes in first 512 bytes).
		if isBinary(path) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})

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
