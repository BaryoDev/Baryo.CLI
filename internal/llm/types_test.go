package llm

import (
	"encoding/json"
	"testing"
)

func TestEndpointIsRemote(t *testing.T) {
	tests := []struct {
		name     string
		endpoint Endpoint
		want     bool
	}{
		{
			name:     "local socket",
			endpoint: Endpoint{SocketPath: "/var/run/docker.sock"},
			want:     false,
		},
		{
			name:     "tcp socket",
			endpoint: Endpoint{SocketPath: "tcp://localhost:11434"},
			want:     false,
		},
		{
			name:     "remote with base URL",
			endpoint: Endpoint{BaseURL: "https://api.openai.com/v1"},
			want:     true,
		},
		{
			name:     "remote with provider",
			endpoint: Endpoint{Provider: "anthropic"},
			want:     true,
		},
		{
			name:     "remote with both",
			endpoint: Endpoint{BaseURL: "https://api.openai.com/v1", Provider: "openai"},
			want:     true,
		},
		{
			name:     "empty endpoint",
			endpoint: Endpoint{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.endpoint.IsRemote(); got != tt.want {
				t.Errorf("Endpoint.IsRemote() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewChatMessage(t *testing.T) {
	msg := NewChatMessage("user", "hello world")
	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if msg.Content == nil {
		t.Fatal("Content is nil, want non-nil")
	}
	if *msg.Content != "hello world" {
		t.Errorf("Content = %q, want %q", *msg.Content, "hello world")
	}
}

func TestNewChatMessageEmptyContent(t *testing.T) {
	msg := NewChatMessage("assistant", "")
	if msg.Content == nil {
		t.Fatal("Content should not be nil even for empty string")
	}
	if *msg.Content != "" {
		t.Errorf("Content = %q, want empty string", *msg.Content)
	}
}

func TestNewMultipartMessage(t *testing.T) {
	images := []ContentPart{
		{Type: "image_url", ImageURL: &ImageURL{URL: "data:image/png;base64,abc"}},
	}
	msg := NewMultipartMessage("user", "describe this", images)

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if len(msg.ContentParts) != 2 {
		t.Fatalf("ContentParts len = %d, want 2", len(msg.ContentParts))
	}
	if msg.ContentParts[0].Type != "text" {
		t.Errorf("first part type = %q, want %q", msg.ContentParts[0].Type, "text")
	}
	if msg.ContentParts[0].Text != "describe this" {
		t.Errorf("first part text = %q, want %q", msg.ContentParts[0].Text, "describe this")
	}
	if msg.ContentParts[1].Type != "image_url" {
		t.Errorf("second part type = %q, want %q", msg.ContentParts[1].Type, "image_url")
	}
}

func TestChatMessageMarshalJSON_PlainText(t *testing.T) {
	msg := NewChatMessage("user", "hello")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result["role"] != "user" {
		t.Errorf("role = %v, want %q", result["role"], "user")
	}
	// Content should be a plain string, not an array
	content, ok := result["content"].(string)
	if !ok {
		t.Fatalf("content is not a string: %T", result["content"])
	}
	if content != "hello" {
		t.Errorf("content = %q, want %q", content, "hello")
	}
}

func TestChatMessageMarshalJSON_Multipart(t *testing.T) {
	images := []ContentPart{
		{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/img.png"}},
	}
	msg := NewMultipartMessage("user", "look at this", images)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Content should be an array when ContentParts is non-empty
	contentArr, ok := result["content"].([]interface{})
	if !ok {
		t.Fatalf("content is not an array: %T", result["content"])
	}
	if len(contentArr) != 2 {
		t.Fatalf("content array len = %d, want 2", len(contentArr))
	}
}

func TestChatMessageMarshalJSON_NilContent(t *testing.T) {
	msg := ChatMessage{Role: "assistant"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result["content"] != nil {
		t.Errorf("content = %v, want nil", result["content"])
	}
}

func TestChatMessageMarshalJSON_WithToolCalls(t *testing.T) {
	content := "thinking..."
	msg := ChatMessage{
		Role:    "assistant",
		Content: &content,
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"main.go"}`,
				},
			},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	toolCalls, ok := result["tool_calls"].([]interface{})
	if !ok {
		t.Fatalf("tool_calls is not an array: %T", result["tool_calls"])
	}
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(toolCalls))
	}
}

func TestChatMessageMarshalJSON_ToolCallID(t *testing.T) {
	content := "file contents here"
	msg := ChatMessage{
		Role:       "tool",
		Content:    &content,
		ToolCallID: "call_123",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result["tool_call_id"] != "call_123" {
		t.Errorf("tool_call_id = %v, want %q", result["tool_call_id"], "call_123")
	}
}

func TestChatMessageMarshalJSON_OmitsEmpty(t *testing.T) {
	msg := NewChatMessage("user", "hello")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// These should be omitted (omitempty)
	if _, ok := result["name"]; ok {
		t.Error("name should be omitted when empty")
	}
	if _, ok := result["tool_calls"]; ok {
		t.Error("tool_calls should be omitted when empty")
	}
	if _, ok := result["tool_call_id"]; ok {
		t.Error("tool_call_id should be omitted when empty")
	}
}
