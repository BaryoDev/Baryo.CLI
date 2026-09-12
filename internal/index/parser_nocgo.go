// SPDX-License-Identifier: MIT

//go:build !cgo

package index

import (
	"fmt"
	"os"
	"time"
)

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

// ParseFile returns file metadata without symbol extraction when CGO is disabled.
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
	}, fmt.Errorf("tree-sitter parsing requires CGO")
}
