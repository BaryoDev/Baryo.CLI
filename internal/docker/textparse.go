// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

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
	reXMLToolCall = regexp.MustCompile(`<tool_call>\s*(\{.*?\})\s*</tool_call>`)

	// 2. Function call syntax: read_file({"path": "main.go"})
	reFuncCall = regexp.MustCompile(`([a-z_][a-z0-9_]*)\((\{.*?\})\)`)

	// 3. Bare JSON: {"name": "read_file", "arguments": {"path": "main.go"}}
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
			continue
		}
		if !validNames[tc.Name] {
			continue
		}
		args := string(tc.Arguments)
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

	// Pattern 2: Function call syntax
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

	// Pattern 3: Bare JSON
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
			continue
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

// buildValidToolNames extracts tool function names from definitions.
func buildValidToolNames(defs []ToolDefinition) map[string]bool {
	names := make(map[string]bool, len(defs))
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	return names
}
