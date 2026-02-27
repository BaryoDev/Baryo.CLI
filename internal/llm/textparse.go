// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

import (
	"encoding/json"
	"regexp"
	"strings"
)

// TextToolCall represents a tool call parsed from the model's text output.
type TextToolCall struct {
	Name      string
	Arguments string
	Raw       string // original matched substring for stripping
}

// Regex patterns tried in priority order (first match wins per occurrence).
var (
	// 1. XML-wrapped: <tool_call>{"name": "read_file", "arguments": {"path": "main.go"}}</tool_call>
	reXMLToolCall = regexp.MustCompile(`(?s)<tool_call>\s*(\{.*?\})\s*</tool_call>`)

	// 2. Named XML tag: <glob>{"pattern":"**/*.go"}</glob>, <list_directory></list_directory>, <list_directory/>
	reNamedXMLCall = regexp.MustCompile(`(?s)<([a-z_][a-z0-9_]*)>\s*(.*?)\s*</([a-z_][a-z0-9_]*)>|<([a-z_][a-z0-9_]*)\s*/>`)

	// 3. Function call syntax: read_file({"path": "main.go"})
	reFuncCall = regexp.MustCompile(`([a-z_][a-z0-9_]*)\((\{.*?\})\)`)

	// 4. Bare JSON: {"name": "read_file", "arguments": {"path": "main.go"}}
	reBareJSON = regexp.MustCompile(`\{\s*"name"\s*:\s*"([a-z_][a-z0-9_]*)"\s*,\s*"arguments"\s*:\s*(\{.*?\})\s*\}`)
)

// toolCallJSON is used to unmarshal XML-wrapped and bare JSON patterns.
type toolCallJSON struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// parseTextToolCalls scans text for tool call patterns and returns matches.
// Only tool names present in validNames are accepted (security gate).
func parseTextToolCalls(text string, validNames map[string]bool) []TextToolCall {
	var calls []TextToolCall

	// Track which byte ranges have already been matched to avoid overlaps.
	type match struct{ start, end int }
	var matched []match

	overlaps := func(s, e int) bool {
		for _, m := range matched {
			if s < m.end && e > m.start {
				return true
			}
		}
		return false
	}

	// Pattern 1: XML-wrapped
	for _, loc := range reXMLToolCall.FindAllStringSubmatchIndex(text, -1) {
		fullStart, fullEnd := loc[0], loc[1]
		if overlaps(fullStart, fullEnd) {
			continue
		}
		jsonStr := text[loc[2]:loc[3]]
		var tc toolCallJSON
		if err := json.Unmarshal([]byte(jsonStr), &tc); err != nil {
			// Small models often output raw newlines inside JSON strings.
			// Try sanitizing before giving up.
			sanitized := sanitizeJSONStrings(jsonStr)
			if err2 := json.Unmarshal([]byte(sanitized), &tc); err2 != nil {
				continue
			}
			jsonStr = sanitized
		}
		if !validNames[tc.Name] {
			continue
		}
		args := string(tc.Arguments)
		if args == "" || args == "null" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			continue
		}
		calls = append(calls, TextToolCall{
			Name:      tc.Name,
			Arguments: args,
			Raw:       text[fullStart:fullEnd],
		})
		matched = append(matched, match{fullStart, fullEnd})
	}

	// Pattern 2: Named XML tag (e.g. <glob>{"pattern":"**/*"}</glob>, <list_directory></list_directory>)
	for _, loc := range reNamedXMLCall.FindAllStringSubmatchIndex(text, -1) {
		fullStart, fullEnd := loc[0], loc[1]
		if overlaps(fullStart, fullEnd) {
			continue
		}

		var name, body string
		if loc[2] >= 0 {
			// Matched <name>body</name>
			open := text[loc[2]:loc[3]]
			closeTag := text[loc[6]:loc[7]]
			if open != closeTag {
				continue
			}
			name = open
			body = strings.TrimSpace(text[loc[4]:loc[5]])
		} else {
			// Matched <name/>
			name = text[loc[8]:loc[9]]
		}

		name = normalizeToolName(name)
		if !validNames[name] {
			continue
		}

		args := parseFlexibleArgs(body)
		if args == "" {
			continue
		}

		calls = append(calls, TextToolCall{
			Name:      name,
			Arguments: args,
			Raw:       text[fullStart:fullEnd],
		})
		matched = append(matched, match{fullStart, fullEnd})
	}

	// Pattern 3: Function call syntax (e.g. read_file({"path":"main.go"}))
	for _, loc := range reFuncCall.FindAllStringSubmatchIndex(text, -1) {
		fullStart, fullEnd := loc[0], loc[1]
		if overlaps(fullStart, fullEnd) {
			continue
		}
		name := text[loc[2]:loc[3]]
		if !validNames[name] {
			continue
		}
		args := text[loc[4]:loc[5]]
		if !json.Valid([]byte(args)) {
			continue
		}
		calls = append(calls, TextToolCall{
			Name:      name,
			Arguments: args,
			Raw:       text[fullStart:fullEnd],
		})
		matched = append(matched, match{fullStart, fullEnd})
	}

	// Pattern 4: Bare JSON
	for _, loc := range reBareJSON.FindAllStringSubmatchIndex(text, -1) {
		fullStart, fullEnd := loc[0], loc[1]
		if overlaps(fullStart, fullEnd) {
			continue
		}
		name := text[loc[2]:loc[3]]
		if !validNames[name] {
			continue
		}
		args := text[loc[4]:loc[5]]
		if !json.Valid([]byte(args)) {
			// Try sanitizing raw control characters.
			args = sanitizeJSONStrings(args)
			if !json.Valid([]byte(args)) {
				continue
			}
		}
		calls = append(calls, TextToolCall{
			Name:      name,
			Arguments: args,
			Raw:       text[fullStart:fullEnd],
		})
		matched = append(matched, match{fullStart, fullEnd})
	}

	return calls
}

// stripToolCallText removes matched tool call substrings from content.
func stripToolCallText(content string, calls []TextToolCall) string {
	for _, c := range calls {
		content = strings.Replace(content, c.Raw, "", 1)
	}
	return strings.TrimSpace(content)
}

// parseFlexibleArgs tries to interpret body as valid JSON arguments.
// Handles: empty body, valid JSON object, bare key-values like "path": ".".
// Returns valid JSON string or empty string if unparseable.
func parseFlexibleArgs(body string) string {
	if body == "" {
		return "{}"
	}
	// Already a valid JSON object.
	if json.Valid([]byte(body)) {
		// Must be an object, not a string/number/array.
		if strings.HasPrefix(body, "{") {
			return body
		}
	}
	// Try wrapping bare key-values in braces: "path": "." → {"path": "."}
	wrapped := "{" + body + "}"
	if json.Valid([]byte(wrapped)) {
		return wrapped
	}
	return ""
}

// normalizeToolName strips common suffixes models add to tool names.
func normalizeToolName(name string) string {
	for _, suffix := range []string{"_call", "_tool"} {
		trimmed := strings.TrimSuffix(name, suffix)
		if trimmed != name && trimmed != "" {
			return trimmed
		}
	}
	return name
}

// buildValidToolNames extracts tool function names from definitions.
func buildValidToolNames(defs []ToolDefinition) map[string]bool {
	names := make(map[string]bool, len(defs))
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	return names
}

// sanitizeJSONStrings escapes raw control characters (newlines, tabs, etc.)
// that appear inside JSON string values. Small models often emit raw newlines
// in code strings instead of proper \n escapes, causing json.Unmarshal to fail.
func sanitizeJSONStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 64)

	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}

		if c == '\\' && inString {
			b.WriteByte(c)
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}

		// Escape raw control characters inside strings.
		if inString && c < 0x20 {
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				// Other control chars: use unicode escape.
				b.WriteString(`\u00`)
				b.WriteByte("0123456789abcdef"[c>>4])
				b.WriteByte("0123456789abcdef"[c&0x0f])
			}
			continue
		}

		b.WriteByte(c)
	}

	return b.String()
}
