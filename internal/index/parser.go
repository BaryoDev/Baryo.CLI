// SPDX-License-Identifier: MIT

//go:build cgo

package index

import (
	"context"
	"fmt"
	"os"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// SymbolsAvailable reports whether this build can extract symbols. Under cgo,
// symbols are extracted via go-tree-sitter.
const SymbolsAvailable = true

// sitterNode wraps a cgo go-tree-sitter Node to satisfy the astNode interface.
type sitterNode struct {
	n *sitter.Node
}

func (s *sitterNode) Type() string {
	if s == nil || s.n == nil {
		return ""
	}
	return s.n.Type()
}

func (s *sitterNode) ChildCount() int {
	if s == nil || s.n == nil {
		return 0
	}
	return int(s.n.ChildCount())
}

func (s *sitterNode) Child(i int) astNode {
	if s == nil || s.n == nil {
		return nil
	}
	c := s.n.Child(i)
	if c == nil {
		return nil
	}
	return &sitterNode{n: c}
}

func (s *sitterNode) ChildByFieldName(name string) astNode {
	if s == nil || s.n == nil {
		return nil
	}
	c := s.n.ChildByFieldName(name)
	if c == nil {
		return nil
	}
	return &sitterNode{n: c}
}

func (s *sitterNode) StartPointRow() int {
	if s == nil || s.n == nil {
		return 0
	}
	return int(s.n.StartPoint().Row)
}

func (s *sitterNode) StartByte() uint32 {
	if s == nil || s.n == nil {
		return 0
	}
	return s.n.StartByte()
}

func (s *sitterNode) EndByte() uint32 {
	if s == nil || s.n == nil {
		return 0
	}
	return s.n.EndByte()
}

// langParser wraps a tree-sitter parser and language-specific extraction logic.
type langParser struct {
	lang    *sitter.Language
	extract func(root astNode, src []byte) []Symbol
}

// newGoParser creates a parser for Go source files.
func newGoParser() *langParser {
	return &langParser{
		lang:    golang.GetLanguage(),
		extract: extractGo,
	}
}

// newJSParser creates a parser for JavaScript source files.
func newJSParser() *langParser {
	return &langParser{
		lang:    javascript.GetLanguage(),
		extract: extractJS,
	}
}

// newTSParser creates a parser for TypeScript source files.
func newTSParser() *langParser {
	return &langParser{
		lang:    typescript.GetLanguage(),
		extract: extractTS,
	}
}

// newPythonParser creates a parser for Python source files.
func newPythonParser() *langParser {
	return &langParser{
		lang:    python.GetLanguage(),
		extract: extractPython,
	}
}

// newRustParser creates a parser for Rust source files.
func newRustParser() *langParser {
	return &langParser{
		lang:    rust.GetLanguage(),
		extract: extractRust,
	}
}

// newJavaParser creates a parser for Java source files.
func newJavaParser() *langParser {
	return &langParser{
		lang:    java.GetLanguage(),
		extract: extractJava,
	}
}

// newCParser creates a parser for C source files.
func newCParser() *langParser {
	return &langParser{
		lang:    c.GetLanguage(),
		extract: extractC,
	}
}

// newCPPParser creates a parser for C++ source files.
func newCPPParser() *langParser {
	return &langParser{
		lang:    cpp.GetLanguage(),
		extract: extractCPP,
	}
}

// ParseFile parses a source file and extracts symbols.
func ParseFile(path, language string, content []byte) (*FileSymbols, error) {
	parsers := map[string]*langParser{
		"go":         newGoParser(),
		"javascript": newJSParser(),
		"typescript": newTSParser(),
		"python":     newPythonParser(),
		"rust":       newRustParser(),
		"java":       newJavaParser(),
		"c":          newCParser(),
		"cpp":        newCPPParser(),
	}

	lp, ok := parsers[language]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lp.lang)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil || tree == nil {
		return nil, fmt.Errorf("failed to parse %s", path)
	}
	defer tree.Close()

	root := tree.RootNode()
	symbols := lp.extract(&sitterNode{n: root}, content)

	info, err := os.Stat(path)
	modTime := time.Time{}
	var size int64
	if err == nil {
		modTime = info.ModTime()
		size = info.Size()
	}

	return &FileSymbols{
		Path:    path,
		Symbols: symbols,
		ModTime: modTime,
		Size:    size,
	}, nil
}
