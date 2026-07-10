// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := WriteFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("read back %q, %v", data, err)
	}

	// Overwrite an existing file.
	if err := WriteFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "second" {
		t.Fatalf("read back %q, want %q", data, "second")
	}

	// No stray temp files left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file in dir, found %d", len(entries))
	}
}

func TestWriteFileAtomicBadDir(t *testing.T) {
	err := WriteFileAtomic(filepath.Join(t.TempDir(), "missing", "out.txt"), []byte("x"), 0o600)
	if err == nil {
		t.Error("expected error writing into missing directory")
	}
}
