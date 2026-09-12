// SPDX-License-Identifier: MIT

//go:build !cgo

package index

import (
	"os"
	"time"
)

// SymbolsAvailable reports whether this build can extract symbols. See the cgo
// build of this file for the full comment.
const SymbolsAvailable = false

// langParser is a stub when CGO is disabled (tree-sitter requires CGO).
type langParser struct{}

func newGoParser() *langParser     { return &langParser{} }
func newJSParser() *langParser     { return &langParser{} }
func newTSParser() *langParser     { return &langParser{} }
func newPythonParser() *langParser { return &langParser{} }
func newRustParser() *langParser   { return &langParser{} }
func newJavaParser() *langParser   { return &langParser{} }
func newCParser() *langParser      { return &langParser{} }
func newCPPParser() *langParser    { return &langParser{} }

// ParseFile returns file metadata without symbol extraction when CGO is
// disabled.
//
// It returns a nil error on purpose. Build and Update treat an error as
// "unparseable" and skip the file, so returning one here dropped every file
// from the index and released binaries, which goreleaser builds with
// CGO_ENABLED=0, shipped an empty repo map. Missing symbols degrade the map to
// a file list; an error removes the project from the model's view entirely.
func ParseFile(path, language string, content []byte) (*FileSymbols, error) {
	info, err := os.Stat(path)
	modTime := time.Time{}
	var size int64
	if err == nil {
		modTime = info.ModTime()
		size = info.Size()
	}

	_ = language
	_ = content

	return &FileSymbols{
		Path:    path,
		ModTime: modTime,
		Size:    size,
	}, nil
}
