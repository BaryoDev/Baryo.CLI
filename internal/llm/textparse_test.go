package llm

import (
	"testing"
)

func TestParseTextToolCalls_XMLWrapped(t *testing.T) {
	validNames := map[string]bool{"read_file": true, "glob": true}

	text := `Let me read the file.
<tool_call>{"name": "read_file", "arguments": {"path": "main.go"}}</tool_call>
Done!`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Errorf("name = %q, want %q", calls[0].Name, "read_file")
	}
	if calls[0].Arguments != `{"path": "main.go"}` {
		t.Errorf("args = %q", calls[0].Arguments)
	}
}

func TestParseTextToolCalls_NamedXML(t *testing.T) {
	validNames := map[string]bool{"glob": true}

	text := `<glob>{"pattern":"**/*.go"}</glob>`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "glob" {
		t.Errorf("name = %q, want %q", calls[0].Name, "glob")
	}
}

func TestParseTextToolCalls_SelfClosingXML(t *testing.T) {
	validNames := map[string]bool{"list_directory": true}

	text := `<list_directory/>`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "list_directory" {
		t.Errorf("name = %q, want %q", calls[0].Name, "list_directory")
	}
	if calls[0].Arguments != "{}" {
		t.Errorf("args = %q, want %q", calls[0].Arguments, "{}")
	}
}

func TestParseTextToolCalls_FunctionSyntax(t *testing.T) {
	validNames := map[string]bool{"read_file": true}

	text := `I'll read the file: read_file({"path": "main.go"})`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Errorf("name = %q, want %q", calls[0].Name, "read_file")
	}
}

func TestParseTextToolCalls_BareJSON(t *testing.T) {
	validNames := map[string]bool{"glob": true}

	text := `Here's what I need: {"name": "glob", "arguments": {"pattern": "*.go"}}`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "glob" {
		t.Errorf("name = %q, want %q", calls[0].Name, "glob")
	}
}

func TestParseTextToolCalls_InvalidToolName(t *testing.T) {
	validNames := map[string]bool{"read_file": true}

	text := `<tool_call>{"name": "delete_everything", "arguments": {}}</tool_call>`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0 (invalid tool name should be rejected)", len(calls))
	}
}

func TestParseTextToolCalls_MultipleCalls(t *testing.T) {
	validNames := map[string]bool{"read_file": true, "glob": true}

	text := `<tool_call>{"name": "read_file", "arguments": {"path": "a.go"}}</tool_call>
<tool_call>{"name": "glob", "arguments": {"pattern": "*.go"}}</tool_call>`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Errorf("first call name = %q, want %q", calls[0].Name, "read_file")
	}
	if calls[1].Name != "glob" {
		t.Errorf("second call name = %q, want %q", calls[1].Name, "glob")
	}
}

func TestParseTextToolCalls_NoCalls(t *testing.T) {
	validNames := map[string]bool{"read_file": true}

	text := `This is just normal text with no tool calls.`

	calls := parseTextToolCalls(text, validNames)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0", len(calls))
	}
}

func TestStripToolCallText(t *testing.T) {
	content := `Hello <tool_call>{"name":"read_file","arguments":{}}</tool_call> world`
	calls := []TextToolCall{
		{Raw: `<tool_call>{"name":"read_file","arguments":{}}</tool_call>`},
	}
	result := stripToolCallText(content, calls)
	if result != "Hello  world" {
		t.Errorf("stripToolCallText = %q, want %q", result, "Hello  world")
	}
}

func TestParseFlexibleArgs(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"empty body", "", "{}"},
		{"valid json object", `{"path":"main.go"}`, `{"path":"main.go"}`},
		{"bare key-values", `"path": "main.go"`, `{"path": "main.go"}`},
		{"invalid json", "not json at all", ""},
		{"json array", `["a","b"]`, ""},
		{"json string", `"hello"`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFlexibleArgs(tt.body)
			if got != tt.want {
				t.Errorf("parseFlexibleArgs(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"read_file", "read_file"},
		{"read_file_call", "read_file"},
		{"read_file_tool", "read_file"},
		{"glob", "glob"},
		{"glob_call", "glob"},
		// Should not strip if result would be empty
		{"_call", "_call"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeToolName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeToolName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildValidToolNames(t *testing.T) {
	defs := []ToolDefinition{
		{Function: FunctionDefinition{Name: "read_file"}},
		{Function: FunctionDefinition{Name: "glob"}},
		{Function: FunctionDefinition{Name: "edit_file"}},
	}
	names := buildValidToolNames(defs)

	if len(names) != 3 {
		t.Errorf("got %d names, want 3", len(names))
	}
	for _, expected := range []string{"read_file", "glob", "edit_file"} {
		if !names[expected] {
			t.Errorf("missing expected tool name %q", expected)
		}
	}
	if names["unknown"] {
		t.Error("unexpected tool name 'unknown' found")
	}
}

func TestSanitizeJSONStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no control chars",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "raw newline in string",
			input: "{\"key\": \"line1\nline2\"}",
			want:  `{"key": "line1\nline2"}`,
		},
		{
			name:  "raw tab in string",
			input: "{\"key\": \"col1\tcol2\"}",
			want:  `{"key": "col1\tcol2"}`,
		},
		{
			name:  "already escaped newline",
			input: `{"key": "line1\nline2"}`,
			want:  `{"key": "line1\nline2"}`,
		},
		{
			name:  "control chars outside strings unchanged",
			input: "{\n\"key\": \"value\"\n}",
			want:  "{\n\"key\": \"value\"\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeJSONStrings(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeJSONStrings() = %q, want %q", got, tt.want)
			}
		})
	}
}
