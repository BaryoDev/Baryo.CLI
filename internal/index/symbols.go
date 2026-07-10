// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package index

import (
	"fmt"
	"strings"
	"time"
)

// SymbolKind classifies extracted code symbols.
type SymbolKind int

const (
	KindFunction SymbolKind = iota
	KindMethod
	KindType
	KindClass
	KindInterface
)

// Symbol represents a single extracted code symbol.
type Symbol struct {
	Kind      SymbolKind
	Name      string // e.g. "StreamChat"
	Signature string // e.g. "func StreamChat(ctx, endpoint, model) <-chan StreamEvent"
	Receiver  string // Go method receiver: "ChatModel"
	Parent    string // parent class for methods
	Line      int
}

// FileSymbols holds all extracted symbols for a single file.
type FileSymbols struct {
	Path    string // relative path from project root
	Symbols []Symbol
	ModTime time.Time
	Size    int64
}

// Format returns a compact multi-line representation of the file and its symbols.
//
//	internal/llm/client.go
//	  func StreamChat(ctx, endpoint, model, messages, params) <-chan StreamEvent
//	  func IsRemoteSocket(path string) bool
func (fs *FileSymbols) Format() string {
	var b strings.Builder
	b.WriteString(fs.Path)
	for _, s := range fs.Symbols {
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(s.Signature)
	}
	return b.String()
}

// FormatShort returns the file path with symbol names only (no signatures).
func (fs *FileSymbols) FormatShort() string {
	var b strings.Builder
	b.WriteString(fs.Path)
	for _, s := range fs.Symbols {
		b.WriteByte('\n')
		b.WriteString("  ")
		switch s.Kind {
		case KindFunction:
			b.WriteString("func ")
		case KindMethod:
			if s.Receiver != "" {
				fmt.Fprintf(&b, "func (%s) ", s.Receiver)
			} else if s.Parent != "" {
				fmt.Fprintf(&b, "%s.", s.Parent)
			}
		case KindType:
			b.WriteString("type ")
		case KindClass:
			b.WriteString("class ")
		case KindInterface:
			b.WriteString("interface ")
		}
		b.WriteString(s.Name)
	}
	return b.String()
}

// FormatTopLevel returns the file path with only top-level symbols (no methods).
func (fs *FileSymbols) FormatTopLevel() string {
	var b strings.Builder
	b.WriteString(fs.Path)
	for _, s := range fs.Symbols {
		if s.Kind == KindMethod {
			continue
		}
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(s.Signature)
	}
	return b.String()
}
