// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package procutil provides shared subprocess helpers.
package procutil

import "bytes"

// CappedBuffer is an io.Writer that stores at most Max bytes and discards
// the rest, so runaway subprocess output cannot exhaust memory. Use it as
// cmd.Stdout/cmd.Stderr in place of CombinedOutput.
type CappedBuffer struct {
	Max       int
	buf       bytes.Buffer
	truncated bool
}

func (b *CappedBuffer) Write(p []byte) (int, error) {
	remaining := b.Max - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *CappedBuffer) String() string { return b.buf.String() }

// Truncated reports whether any output was discarded.
func (b *CappedBuffer) Truncated() bool { return b.truncated }
