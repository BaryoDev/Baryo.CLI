// SPDX-License-Identifier: MIT

//go:build !cgo

package index

import (
	"fmt"
	"os"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// SymbolsAvailable reports whether this build can extract symbols.
// Under pure-Go (!cgo), symbols are extracted via gotreesitter.
const SymbolsAvailable = true

// maxSymbolParseSize caps the files the pure-Go parser reads for symbols. It
// allocates far more than the cgo parser: a 1 MiB file, which DiscoverFiles
// allows, measured about 450 MiB of heap. Larger files stay in the index
// without symbols, and the RAG store chunks them by lines instead.
const maxSymbolParseSize = 256 << 10

// gtsNode wraps a pure-Go gotreesitter Node to satisfy the astNode interface.
type gtsNode struct {
	n    *gotreesitter.Node
	lang *gotreesitter.Language
}

func (g *gtsNode) Type() string {
	if g == nil || g.n == nil {
		return ""
	}
	return g.n.Type(g.lang)
}

func (g *gtsNode) ChildCount() int {
	if g == nil || g.n == nil {
		return 0
	}
	return g.n.ChildCount()
}

func (g *gtsNode) Child(i int) astNode {
	if g == nil || g.n == nil {
		return nil
	}
	c := g.n.Child(i)
	if c == nil {
		return nil
	}
	return &gtsNode{n: c, lang: g.lang}
}

func (g *gtsNode) ChildByFieldName(name string) astNode {
	if g == nil || g.n == nil {
		return nil
	}
	c := g.n.ChildByFieldName(name, g.lang)
	if c == nil {
		return nil
	}
	return &gtsNode{n: c, lang: g.lang}
}

func (g *gtsNode) StartPointRow() int {
	if g == nil || g.n == nil {
		return 0
	}
	return int(g.n.StartPoint().Row)
}

func (g *gtsNode) StartByte() uint32 {
	if g == nil || g.n == nil {
		return 0
	}
	return g.n.StartByte()
}

func (g *gtsNode) EndByte() uint32 {
	if g == nil || g.n == nil {
		return 0
	}
	return g.n.EndByte()
}

// langParser wraps a tree-sitter parser and language-specific extraction logic.
type langParser struct {
	lang    *gotreesitter.Language
	extract func(root astNode, src []byte) []Symbol
}

// newGoParser creates a pure-Go parser for Go source files.
func newGoParser() *langParser {
	return &langParser{
		lang:    grammars.GoLanguage(),
		extract: extractGo,
	}
}

// newJSParser creates a pure-Go parser for JavaScript source files.
func newJSParser() *langParser {
	return &langParser{
		lang:    grammars.JavascriptLanguage(),
		extract: extractJS,
	}
}

// newTSParser creates a pure-Go parser for TypeScript source files.
func newTSParser() *langParser {
	return &langParser{
		lang:    grammars.TypescriptLanguage(),
		extract: extractTS,
	}
}

// newPythonParser creates a pure-Go parser for Python source files.
func newPythonParser() *langParser {
	return &langParser{
		lang:    grammars.PythonLanguage(),
		extract: extractPython,
	}
}

// newRustParser creates a pure-Go parser for Rust source files.
func newRustParser() *langParser {
	return &langParser{
		lang:    grammars.RustLanguage(),
		extract: extractRust,
	}
}

// newJavaParser creates a pure-Go parser for Java source files.
func newJavaParser() *langParser {
	return &langParser{
		lang:    grammars.JavaLanguage(),
		extract: extractJava,
	}
}

// newCParser creates a pure-Go parser for C source files.
func newCParser() *langParser {
	return &langParser{
		lang:    grammars.CLanguage(),
		extract: extractC,
	}
}

// newCPPParser creates a pure-Go parser for C++ source files.
func newCPPParser() *langParser {
	return &langParser{
		lang:    grammars.CppLanguage(),
		extract: extractCPP,
	}
}

// ParseFile parses a source file and extracts symbols in pure Go.
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

	if len(content) > maxSymbolParseSize {
		return fileWithoutSymbols(path), nil
	}

	parser := gotreesitter.NewParser(lp.lang)
	tree, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if tree == nil {
		return nil, fmt.Errorf("failed to parse %s", path)
	}

	root := tree.RootNode()
	fs := fileWithoutSymbols(path)
	fs.Symbols = lp.extract(&gtsNode{n: root, lang: lp.lang}, content)
	return fs, nil
}

func fileWithoutSymbols(path string) *FileSymbols {
	fs := &FileSymbols{Path: path}
	if info, err := os.Stat(path); err == nil {
		fs.ModTime = info.ModTime()
		fs.Size = info.Size()
	}
	return fs
}
