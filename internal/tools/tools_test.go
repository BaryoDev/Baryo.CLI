package tools

import (
	"context"
	"testing"
)

func TestSanitizeArgs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single valid object",
			input: `{"path":"main.go"}`,
			want:  `{"path":"main.go"}`,
		},
		{
			name:  "concatenated objects (Gemini bug)",
			input: `{"path":"a.go"}{"path":"b.go"}`,
			want:  `{"path":"a.go"}`,
		},
		{
			name:  "invalid JSON passthrough",
			input: `not json`,
			want:  `not json`,
		},
		{
			name:  "empty string",
			input: ``,
			want:  ``,
		},
		{
			name:  "nested object",
			input: `{"key":{"nested":"value"}}`,
			want:  `{"key":{"nested":"value"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeArgs(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeArgs(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	result := Execute(context.Background(), "nonexistent_tool", "{}")
	if !result.IsError {
		t.Error("Execute with unknown tool should return IsError=true")
	}
	if result.Content == "" {
		t.Error("Execute with unknown tool should return error message")
	}
}

func TestRegisterAndExecute(t *testing.T) {
	// Register a test tool
	Register("test_tool", Tool{
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "test_tool",
				Description: "A test tool",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		},
		Execute: func(ctx context.Context, argsJSON string) Result {
			return Result{Content: "test result", IsError: false}
		},
		Destructive: false,
	})

	// Execute it
	result := Execute(context.Background(), "test_tool", "{}")
	if result.IsError {
		t.Errorf("Execute returned error: %s", result.Content)
	}
	if result.Content != "test result" {
		t.Errorf("Content = %q, want %q", result.Content, "test result")
	}

	// Check IsDestructive
	if IsDestructive("test_tool") {
		t.Error("test_tool should not be destructive")
	}

	// Clean up
	delete(registry, "test_tool")
}

func TestIsDestructive_UnknownTool(t *testing.T) {
	if IsDestructive("nonexistent") {
		t.Error("unknown tool should not be destructive")
	}
}

func TestNames(t *testing.T) {
	// Save and restore registry
	orig := make(map[string]Tool)
	for k, v := range registry {
		orig[k] = v
	}
	defer func() {
		registry = orig
	}()

	// Clear and add known tools
	registry = map[string]Tool{
		"tool_a": {},
		"tool_b": {},
	}

	names := Names()
	if len(names) != 2 {
		t.Fatalf("Names() returned %d items, want 2", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["tool_a"] || !nameSet["tool_b"] {
		t.Errorf("Names() = %v, want [tool_a, tool_b]", names)
	}
}

func TestAllDefinitions(t *testing.T) {
	defs := AllDefinitions()
	// Just verify it doesn't panic and returns something
	_ = defs
}

func TestDockerDefinitions(t *testing.T) {
	defs := DockerDefinitions()
	for _, d := range defs {
		if d.Type == "" {
			t.Error("DockerDefinitions entry has empty Type")
		}
	}
}

func TestReadOnlyDefinitions(t *testing.T) {
	readOnly := ReadOnlyDefinitions()
	all := AllDefinitions()
	if len(readOnly) > len(all) {
		t.Error("ReadOnlyDefinitions should not return more than AllDefinitions")
	}
}
