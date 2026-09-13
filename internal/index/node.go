// SPDX-License-Identifier: MIT

package index

// astNode abstracts an AST node across parser implementations (CGO go-tree-sitter
// and pure-Go gotreesitter).
type astNode interface {
	Type() string
	ChildCount() int
	Child(i int) astNode
	ChildByFieldName(name string) astNode
	StartPointRow() int
	StartByte() uint32
	EndByte() uint32
}

// nodeText returns the source text of an AST node.
func nodeText(n astNode, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

// childByField returns the first child node with the given field name, or nil.
func childByField(n astNode, name string) astNode {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name)
}
