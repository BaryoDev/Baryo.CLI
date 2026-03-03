// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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
