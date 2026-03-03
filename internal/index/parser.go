// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

//go:build cgo

package index

import (
	"fmt"
	"os"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// langParser wraps a tree-sitter parser and language-specific extraction logic.
type langParser struct {
	lang    *sitter.Language
	extract func(root *sitter.Node, src []byte) []Symbol
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

// ParseFile parses a source file and extracts symbols.
func ParseFile(path, language string, content []byte) (*FileSymbols, error) {
	parsers := map[string]*langParser{
		"go":         newGoParser(),
		"javascript": newJSParser(),
		"typescript": newTSParser(),
		"python":     newPythonParser(),
	}

	lp, ok := parsers[language]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lp.lang)

	tree := parser.Parse(nil, content)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse %s", path)
	}
	defer tree.Close()

	root := tree.RootNode()
	symbols := lp.extract(root, content)

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

// nodeText returns the source text of a tree-sitter node.
func nodeText(n *sitter.Node, src []byte) string {
	return string(src[n.StartByte():n.EndByte()])
}

// childByField returns the first child node with the given field name, or nil.
func childByField(n *sitter.Node, name string) *sitter.Node {
	return n.ChildByFieldName(name)
}

// extractGo extracts symbols from a Go AST.
func extractGo(root *sitter.Node, src []byte) []Symbol {
	var symbols []Symbol

	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_declaration":
			symbols = append(symbols, goFunction(child, src))
		case "method_declaration":
			symbols = append(symbols, goMethod(child, src))
		case "type_declaration":
			// type_declaration has type_spec children
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() == "type_spec" {
					symbols = append(symbols, goTypeSpec(spec, src))
				}
			}
		}
	}
	return symbols
}

func goFunction(n *sitter.Node, src []byte) Symbol {
	name := childByField(n, "name")
	params := childByField(n, "parameters")
	result := childByField(n, "result")

	sig := "func "
	if name != nil {
		sig += nodeText(name, src)
	}
	if params != nil {
		sig += nodeText(params, src)
	}
	if result != nil {
		sig += " " + nodeText(result, src)
	}

	nameStr := ""
	if name != nil {
		nameStr = nodeText(name, src)
	}
	return Symbol{
		Kind:      KindFunction,
		Name:      nameStr,
		Signature: sig,
		Line:      int(n.StartPoint().Row) + 1,
	}
}

func goMethod(n *sitter.Node, src []byte) Symbol {
	name := childByField(n, "name")
	params := childByField(n, "parameters")
	result := childByField(n, "result")
	receiver := childByField(n, "receiver")

	var recvType string
	if receiver != nil {
		recvType = extractGoReceiver(receiver, src)
	}

	sig := "func "
	if receiver != nil {
		sig += nodeText(receiver, src) + " "
	}
	if name != nil {
		sig += nodeText(name, src)
	}
	if params != nil {
		sig += nodeText(params, src)
	}
	if result != nil {
		sig += " " + nodeText(result, src)
	}

	nameStr := ""
	if name != nil {
		nameStr = nodeText(name, src)
	}
	return Symbol{
		Kind:      KindMethod,
		Name:      nameStr,
		Signature: sig,
		Receiver:  recvType,
		Line:      int(n.StartPoint().Row) + 1,
	}
}

func extractGoReceiver(n *sitter.Node, src []byte) string {
	// Receiver is a parameter_list like (m *ChatModel)
	// Walk to find the type name.
	text := nodeText(n, src)
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	parts := strings.Fields(text)
	if len(parts) >= 2 {
		t := parts[len(parts)-1]
		t = strings.TrimPrefix(t, "*")
		return t
	}
	if len(parts) == 1 {
		t := parts[0]
		t = strings.TrimPrefix(t, "*")
		return t
	}
	return ""
}

func goTypeSpec(n *sitter.Node, src []byte) Symbol {
	name := childByField(n, "name")
	typeNode := childByField(n, "type")

	kind := KindType
	typeName := ""
	if typeNode != nil {
		t := typeNode.Type()
		if t == "interface_type" {
			kind = KindInterface
		}
		typeName = t
	}

	nameStr := ""
	if name != nil {
		nameStr = nodeText(name, src)
	}

	sig := "type " + nameStr
	switch {
	case typeName == "struct_type":
		sig += " struct"
	case typeName == "interface_type":
		sig += " interface"
	case typeNode != nil:
		sig += " " + nodeText(typeNode, src)
		// Truncate long type definitions
		if len(sig) > 120 {
			sig = sig[:117] + "..."
		}
	}

	return Symbol{
		Kind:      kind,
		Name:      nameStr,
		Signature: sig,
		Line:      int(n.StartPoint().Row) + 1,
	}
}

// extractJS extracts symbols from a JavaScript AST.
func extractJS(root *sitter.Node, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		symbols = append(symbols, extractJSNode(child, src, "")...)
	}
	return symbols
}

func extractJSNode(n *sitter.Node, src []byte, parentClass string) []Symbol {
	var symbols []Symbol
	switch n.Type() {
	case "function_declaration":
		name := childByField(n, "name")
		params := childByField(n, "parameters")
		nameStr := ""
		if name != nil {
			nameStr = nodeText(name, src)
		}
		sig := "function " + nameStr
		if params != nil {
			sig += nodeText(params, src)
		}
		symbols = append(symbols, Symbol{
			Kind:      KindFunction,
			Name:      nameStr,
			Signature: sig,
			Line:      int(n.StartPoint().Row) + 1,
		})

	case "class_declaration":
		name := childByField(n, "name")
		nameStr := ""
		if name != nil {
			nameStr = nodeText(name, src)
		}
		symbols = append(symbols, Symbol{
			Kind:      KindClass,
			Name:      nameStr,
			Signature: "class " + nameStr,
			Line:      int(n.StartPoint().Row) + 1,
		})
		// Extract methods from class body
		body := childByField(n, "body")
		if body != nil {
			for j := 0; j < int(body.ChildCount()); j++ {
				member := body.Child(j)
				if member.Type() == "method_definition" {
					symbols = append(symbols, jsMethod(member, src, nameStr))
				}
			}
		}

	case "export_statement":
		// Check for exported declarations
		for j := 0; j < int(n.ChildCount()); j++ {
			child := n.Child(j)
			symbols = append(symbols, extractJSNode(child, src, parentClass)...)
		}

	case "lexical_declaration":
		// const foo = () => {} or const foo = function() {}
		for j := 0; j < int(n.ChildCount()); j++ {
			decl := n.Child(j)
			if decl.Type() == "variable_declarator" {
				name := childByField(decl, "name")
				value := childByField(decl, "value")
				if name != nil && value != nil {
					vt := value.Type()
					if vt == "arrow_function" || vt == "function" {
						nameStr := nodeText(name, src)
						params := childByField(value, "parameters")
						sig := "const " + nameStr + " = "
						if vt == "arrow_function" {
							if params != nil {
								sig += nodeText(params, src) + " => ..."
							} else {
								sig += "() => ..."
							}
						} else {
							sig += "function"
							if params != nil {
								sig += nodeText(params, src)
							}
						}
						symbols = append(symbols, Symbol{
							Kind:      KindFunction,
							Name:      nameStr,
							Signature: sig,
							Line:      int(decl.StartPoint().Row) + 1,
						})
					}
				}
			}
		}
	}
	return symbols
}

func jsMethod(n *sitter.Node, src []byte, className string) Symbol {
	name := childByField(n, "name")
	params := childByField(n, "parameters")
	nameStr := ""
	if name != nil {
		nameStr = nodeText(name, src)
	}
	sig := nameStr
	if params != nil {
		sig += nodeText(params, src)
	}
	return Symbol{
		Kind:      KindMethod,
		Name:      nameStr,
		Signature: sig,
		Parent:    className,
		Line:      int(n.StartPoint().Row) + 1,
	}
}

// extractTS extracts symbols from a TypeScript AST (JS plus interfaces and type aliases).
func extractTS(root *sitter.Node, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		symbols = append(symbols, extractTSNode(child, src, "")...)
	}
	return symbols
}

func extractTSNode(n *sitter.Node, src []byte, parentClass string) []Symbol {
	var symbols []Symbol
	switch n.Type() {
	case "interface_declaration":
		name := childByField(n, "name")
		nameStr := ""
		if name != nil {
			nameStr = nodeText(name, src)
		}
		symbols = append(symbols, Symbol{
			Kind:      KindInterface,
			Name:      nameStr,
			Signature: "interface " + nameStr,
			Line:      int(n.StartPoint().Row) + 1,
		})

	case "type_alias_declaration":
		name := childByField(n, "name")
		nameStr := ""
		if name != nil {
			nameStr = nodeText(name, src)
		}
		sig := "type " + nameStr
		value := childByField(n, "value")
		if value != nil {
			valText := nodeText(value, src)
			if len(valText) <= 60 {
				sig += " = " + valText
			}
		}
		symbols = append(symbols, Symbol{
			Kind:      KindType,
			Name:      nameStr,
			Signature: sig,
			Line:      int(n.StartPoint().Row) + 1,
		})

	default:
		// Delegate to JS extractor for shared node types
		symbols = append(symbols, extractJSNode(n, src, parentClass)...)
	}
	return symbols
}

// extractPython extracts symbols from a Python AST.
func extractPython(root *sitter.Node, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		symbols = append(symbols, extractPythonNode(child, src, "")...)
	}
	return symbols
}

func extractPythonNode(n *sitter.Node, src []byte, parentClass string) []Symbol {
	var symbols []Symbol
	switch n.Type() {
	case "function_definition":
		name := childByField(n, "name")
		params := childByField(n, "parameters")
		returnType := childByField(n, "return_type")
		nameStr := ""
		if name != nil {
			nameStr = nodeText(name, src)
		}

		kind := KindFunction
		if parentClass != "" {
			kind = KindMethod
		}

		sig := "def " + nameStr
		if params != nil {
			sig += nodeText(params, src)
		}
		if returnType != nil {
			sig += " -> " + nodeText(returnType, src)
		}

		symbols = append(symbols, Symbol{
			Kind:      kind,
			Name:      nameStr,
			Signature: sig,
			Parent:    parentClass,
			Line:      int(n.StartPoint().Row) + 1,
		})

	case "class_definition":
		name := childByField(n, "name")
		nameStr := ""
		if name != nil {
			nameStr = nodeText(name, src)
		}

		superclass := childByField(n, "superclasses")
		sig := "class " + nameStr
		if superclass != nil {
			sig += nodeText(superclass, src)
		}

		symbols = append(symbols, Symbol{
			Kind:      KindClass,
			Name:      nameStr,
			Signature: sig,
			Line:      int(n.StartPoint().Row) + 1,
		})

		// Extract methods from class body
		body := childByField(n, "body")
		if body != nil {
			for j := 0; j < int(body.ChildCount()); j++ {
				member := body.Child(j)
				symbols = append(symbols, extractPythonNode(member, src, nameStr)...)
			}
		}

	case "decorated_definition":
		// unwrap the decorated definition to get the actual function/class
		for j := 0; j < int(n.ChildCount()); j++ {
			child := n.Child(j)
			if child.Type() == "function_definition" || child.Type() == "class_definition" {
				symbols = append(symbols, extractPythonNode(child, src, parentClass)...)
			}
		}
	}
	return symbols
}
