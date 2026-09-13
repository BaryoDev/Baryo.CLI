// SPDX-License-Identifier: MIT

package index

import (
	"strings"
)

func extractGo(root astNode, src []byte) []Symbol {
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

func goFunction(n astNode, src []byte) Symbol {
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
		Line:      int(n.StartPointRow()) + 1,
	}
}

func goMethod(n astNode, src []byte) Symbol {
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
		Line:      int(n.StartPointRow()) + 1,
	}
}

func extractGoReceiver(n astNode, src []byte) string {
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

func goTypeSpec(n astNode, src []byte) Symbol {
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
		Line:      int(n.StartPointRow()) + 1,
	}
}

// extractJS extracts symbols from a JavaScript AST.
func extractJS(root astNode, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		symbols = append(symbols, extractJSNode(child, src, "")...)
	}
	return symbols
}

func extractJSNode(n astNode, src []byte, parentClass string) []Symbol {
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
			Line:      int(n.StartPointRow()) + 1,
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
			Line:      int(n.StartPointRow()) + 1,
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
							Line:      int(decl.StartPointRow()) + 1,
						})
					}
				}
			}
		}
	}
	return symbols
}

func jsMethod(n astNode, src []byte, className string) Symbol {
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
		Line:      int(n.StartPointRow()) + 1,
	}
}

// extractTS extracts symbols from a TypeScript AST (JS plus interfaces and type aliases).
func extractTS(root astNode, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		symbols = append(symbols, extractTSNode(child, src, "")...)
	}
	return symbols
}

func extractTSNode(n astNode, src []byte, parentClass string) []Symbol {
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
			Line:      int(n.StartPointRow()) + 1,
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
			Line:      int(n.StartPointRow()) + 1,
		})

	default:
		// Delegate to JS extractor for shared node types
		symbols = append(symbols, extractJSNode(n, src, parentClass)...)
	}
	return symbols
}

// extractPython extracts symbols from a Python AST.
func extractPython(root astNode, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		symbols = append(symbols, extractPythonNode(child, src, "")...)
	}
	return symbols
}

func extractPythonNode(n astNode, src []byte, parentClass string) []Symbol {
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
			Line:      int(n.StartPointRow()) + 1,
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
			Line:      int(n.StartPointRow()) + 1,
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

// extractRust extracts symbols from a Rust AST.
func extractRust(root astNode, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_item":
			name := childByField(child, "name")
			params := childByField(child, "parameters")
			returnType := childByField(child, "return_type")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			sig := "fn " + nameStr
			if params != nil {
				sig += nodeText(params, src)
			}
			if returnType != nil {
				sig += " -> " + nodeText(returnType, src)
			}
			symbols = append(symbols, Symbol{
				Kind:      KindFunction,
				Name:      nameStr,
				Signature: sig,
				Line:      int(child.StartPointRow()) + 1,
			})

		case "struct_item":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			symbols = append(symbols, Symbol{
				Kind:      KindType,
				Name:      nameStr,
				Signature: "struct " + nameStr,
				Line:      int(child.StartPointRow()) + 1,
			})

		case "enum_item":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			symbols = append(symbols, Symbol{
				Kind:      KindType,
				Name:      nameStr,
				Signature: "enum " + nameStr,
				Line:      int(child.StartPointRow()) + 1,
			})

		case "trait_item":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			symbols = append(symbols, Symbol{
				Kind:      KindInterface,
				Name:      nameStr,
				Signature: "trait " + nameStr,
				Line:      int(child.StartPointRow()) + 1,
			})

		case "impl_item":
			// Extract methods from impl blocks.
			typeName := childByField(child, "type")
			typeStr := ""
			if typeName != nil {
				typeStr = nodeText(typeName, src)
			}
			body := childByField(child, "body")
			if body != nil {
				for j := 0; j < int(body.ChildCount()); j++ {
					member := body.Child(j)
					if member.Type() == "function_item" {
						name := childByField(member, "name")
						params := childByField(member, "parameters")
						returnType := childByField(member, "return_type")
						nameStr := ""
						if name != nil {
							nameStr = nodeText(name, src)
						}
						sig := "fn " + nameStr
						if params != nil {
							sig += nodeText(params, src)
						}
						if returnType != nil {
							sig += " -> " + nodeText(returnType, src)
						}
						symbols = append(symbols, Symbol{
							Kind:      KindMethod,
							Name:      nameStr,
							Signature: sig,
							Parent:    typeStr,
							Line:      int(member.StartPointRow()) + 1,
						})
					}
				}
			}
		}
	}
	return symbols
}

// extractJava extracts symbols from a Java AST.
func extractJava(root astNode, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		symbols = append(symbols, extractJavaNode(child, src, "")...)
	}
	return symbols
}

func extractJavaNode(n astNode, src []byte, parentClass string) []Symbol {
	var symbols []Symbol
	switch n.Type() {
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
			Line:      int(n.StartPointRow()) + 1,
		})
		// Extract methods from class body.
		body := childByField(n, "body")
		if body != nil {
			for j := 0; j < int(body.ChildCount()); j++ {
				member := body.Child(j)
				symbols = append(symbols, extractJavaNode(member, src, nameStr)...)
			}
		}

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
			Line:      int(n.StartPointRow()) + 1,
		})
		// Extract methods from interface body.
		body := childByField(n, "body")
		if body != nil {
			for j := 0; j < int(body.ChildCount()); j++ {
				member := body.Child(j)
				symbols = append(symbols, extractJavaNode(member, src, nameStr)...)
			}
		}

	case "method_declaration", "constructor_declaration":
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
		// Prepend return type for methods.
		typeNode := childByField(n, "type")
		if typeNode != nil {
			sig = nodeText(typeNode, src) + " " + sig
		}
		symbols = append(symbols, Symbol{
			Kind:      KindMethod,
			Name:      nameStr,
			Signature: sig,
			Parent:    parentClass,
			Line:      int(n.StartPointRow()) + 1,
		})
	}
	return symbols
}

// extractC extracts symbols from a C AST.
func extractC(root astNode, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_definition":
			declarator := childByField(child, "declarator")
			nameStr := extractDeclaratorName(declarator, src)
			sig := strings.TrimSpace(nodeText(child, src))
			// Truncate at opening brace to get just the signature.
			if idx := strings.Index(sig, "{"); idx > 0 {
				sig = strings.TrimSpace(sig[:idx])
			}
			if len(sig) > 120 {
				sig = sig[:117] + "..."
			}
			symbols = append(symbols, Symbol{
				Kind:      KindFunction,
				Name:      nameStr,
				Signature: sig,
				Line:      int(child.StartPointRow()) + 1,
			})

		case "struct_specifier":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			if nameStr != "" {
				symbols = append(symbols, Symbol{
					Kind:      KindType,
					Name:      nameStr,
					Signature: "struct " + nameStr,
					Line:      int(child.StartPointRow()) + 1,
				})
			}

		case "enum_specifier":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			if nameStr != "" {
				symbols = append(symbols, Symbol{
					Kind:      KindType,
					Name:      nameStr,
					Signature: "enum " + nameStr,
					Line:      int(child.StartPointRow()) + 1,
				})
			}

		case "declaration":
			// Top-level declarations may contain struct/enum specifiers.
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() == "struct_specifier" || spec.Type() == "enum_specifier" {
					name := childByField(spec, "name")
					nameStr := ""
					if name != nil {
						nameStr = nodeText(name, src)
					}
					if nameStr != "" {
						kind := KindType
						prefix := "struct "
						if spec.Type() == "enum_specifier" {
							prefix = "enum "
						}
						symbols = append(symbols, Symbol{
							Kind:      kind,
							Name:      nameStr,
							Signature: prefix + nameStr,
							Line:      int(spec.StartPointRow()) + 1,
						})
					}
				}
			}
		}
	}
	return symbols
}

// extractCPP extracts symbols from a C++ AST (extends C with class support).
func extractCPP(root astNode, src []byte) []Symbol {
	var symbols []Symbol
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_definition":
			declarator := childByField(child, "declarator")
			nameStr := extractDeclaratorName(declarator, src)
			sig := strings.TrimSpace(nodeText(child, src))
			if idx := strings.Index(sig, "{"); idx > 0 {
				sig = strings.TrimSpace(sig[:idx])
			}
			if len(sig) > 120 {
				sig = sig[:117] + "..."
			}
			symbols = append(symbols, Symbol{
				Kind:      KindFunction,
				Name:      nameStr,
				Signature: sig,
				Line:      int(child.StartPointRow()) + 1,
			})

		case "class_specifier":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			if nameStr != "" {
				symbols = append(symbols, Symbol{
					Kind:      KindClass,
					Name:      nameStr,
					Signature: "class " + nameStr,
					Line:      int(child.StartPointRow()) + 1,
				})
				// Extract methods from class body.
				body := childByField(child, "body")
				if body != nil {
					for j := 0; j < int(body.ChildCount()); j++ {
						member := body.Child(j)
						if member.Type() == "function_definition" {
							decl := childByField(member, "declarator")
							mName := extractDeclaratorName(decl, src)
							mSig := strings.TrimSpace(nodeText(member, src))
							if idx := strings.Index(mSig, "{"); idx > 0 {
								mSig = strings.TrimSpace(mSig[:idx])
							}
							if len(mSig) > 120 {
								mSig = mSig[:117] + "..."
							}
							symbols = append(symbols, Symbol{
								Kind:      KindMethod,
								Name:      mName,
								Signature: mSig,
								Parent:    nameStr,
								Line:      int(member.StartPointRow()) + 1,
							})
						} else if member.Type() == "declaration" {
							// Method declaration (not definition).
							decl := childByField(member, "declarator")
							mName := extractDeclaratorName(decl, src)
							if mName != "" {
								mSig := strings.TrimSuffix(strings.TrimSpace(nodeText(member, src)), ";")
								symbols = append(symbols, Symbol{
									Kind:      KindMethod,
									Name:      mName,
									Signature: mSig,
									Parent:    nameStr,
									Line:      int(member.StartPointRow()) + 1,
								})
							}
						}
					}
				}
			}

		case "struct_specifier":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			if nameStr != "" {
				symbols = append(symbols, Symbol{
					Kind:      KindType,
					Name:      nameStr,
					Signature: "struct " + nameStr,
					Line:      int(child.StartPointRow()) + 1,
				})
			}

		case "enum_specifier":
			name := childByField(child, "name")
			nameStr := ""
			if name != nil {
				nameStr = nodeText(name, src)
			}
			if nameStr != "" {
				symbols = append(symbols, Symbol{
					Kind:      KindType,
					Name:      nameStr,
					Signature: "enum " + nameStr,
					Line:      int(child.StartPointRow()) + 1,
				})
			}

		case "declaration":
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				switch spec.Type() {
				case "struct_specifier", "enum_specifier":
					name := childByField(spec, "name")
					nameStr := ""
					if name != nil {
						nameStr = nodeText(name, src)
					}
					if nameStr != "" {
						kind := KindType
						prefix := "struct "
						if spec.Type() == "enum_specifier" {
							prefix = "enum "
						}
						symbols = append(symbols, Symbol{
							Kind:      kind,
							Name:      nameStr,
							Signature: prefix + nameStr,
							Line:      int(spec.StartPointRow()) + 1,
						})
					}
				case "class_specifier":
					name := childByField(spec, "name")
					nameStr := ""
					if name != nil {
						nameStr = nodeText(name, src)
					}
					if nameStr != "" {
						symbols = append(symbols, Symbol{
							Kind:      KindClass,
							Name:      nameStr,
							Signature: "class " + nameStr,
							Line:      int(spec.StartPointRow()) + 1,
						})
					}
				}
			}
		}
	}
	return symbols
}

// extractDeclaratorName extracts the function name from a C/C++ declarator node.
func extractDeclaratorName(n astNode, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier", "field_identifier":
		return nodeText(n, src)
	case "qualified_identifier":
		// For C++ qualified names like Foo::bar, use the last part.
		name := childByField(n, "name")
		if name != nil {
			return nodeText(name, src)
		}
		return nodeText(n, src)
	}
	// Walk children to find the identifier (handles function_declarator, pointer_declarator, etc.)
	declarator := childByField(n, "declarator")
	if declarator != nil {
		return extractDeclaratorName(declarator, src)
	}
	// Fallback: walk all children.
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type() == "identifier" || child.Type() == "field_identifier" {
			return nodeText(child, src)
		}
	}
	return ""
}
