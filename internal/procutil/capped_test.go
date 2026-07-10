// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package procutil

import (
	"strings"
	"testing"
)

func TestCappedBufferUnderLimit(t *testing.T) {
	var b CappedBuffer
	b.Max = 10
	n, err := b.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write = %d, %v; want 5, nil", n, err)
	}
	if b.String() != "hello" || b.Truncated() {
		t.Errorf("got %q truncated=%v, want %q truncated=false", b.String(), b.Truncated(), "hello")
	}
}

func TestCappedBufferOverLimit(t *testing.T) {
	var b CappedBuffer
	b.Max = 8
	for range 100 {
		n, err := b.Write([]byte("abcdef"))
		if err != nil || n != 6 {
			t.Fatalf("Write = %d, %v; want 6, nil", n, err)
		}
	}
	if len(b.String()) != 8 {
		t.Errorf("stored %d bytes, want 8", len(b.String()))
	}
	if !b.Truncated() {
		t.Error("expected truncated")
	}
	if !strings.HasPrefix(b.String(), "abcdef") {
		t.Errorf("unexpected content %q", b.String())
	}
}
