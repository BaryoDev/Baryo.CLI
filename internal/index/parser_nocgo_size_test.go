// SPDX-License-Identifier: MIT

//go:build !cgo

package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file over the pure-Go parse cap must stay in the index, without symbols,
// rather than being dropped or parsed at hundreds of MiB of heap.
func TestLargeFileIndexedWithoutSymbols(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("package main\n\n")
	for b.Len() <= maxSymbolParseSize {
		b.WriteString("func Padding() {}\n")
	}
	big := b.String()
	if err := os.WriteFile(filepath.Join(root, "big.go"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "small.go"),
		[]byte("package main\n\nfunc Alpha() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := New(root)
	if err := idx.Build(context.Background()); err != nil {
		t.Fatal(err)
	}

	fs := idx.FileSymbolsFor("big.go")
	if fs == nil {
		t.Fatal("big.go was dropped from the index")
	}
	if len(fs.Symbols) != 0 {
		t.Errorf("big.go is over the cap but produced %d symbols", len(fs.Symbols))
	}
	if fs.Size != int64(len(big)) {
		t.Errorf("big.go size = %d, want %d", fs.Size, len(big))
	}

	small := idx.FileSymbolsFor("small.go")
	if small == nil || len(small.Symbols) == 0 {
		t.Error("small.go is under the cap and should still have symbols")
	}
}
