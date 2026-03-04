// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

import "encoding/json"

// modelRaw matches the actual JSON from `docker model list --json`.
type modelRaw struct {
	ID     string   `json:"id"`
	Tags   []string `json:"tags"`
	Config struct {
		Format       string `json:"format"`
		Quantization string `json:"quantization"`
		Parameters   string `json:"parameters"`
		Architecture string `json:"architecture"`
		Size         string `json:"size"`
	} `json:"config"`
}

// Model is the cleaned-up model info we use in the TUI.
type Model struct {
	Name            string  // e.g. "ai/mistral"
	Tag             string  // full tag e.g. "docker.io/ai/mistral:latest"
	Params          string  // e.g. "7.25 B"
	Size            string  // e.g. "4.07 GiB"
	Quantization    string  // e.g. "Q4_K_M"
	Provider        string  // empty = local, "gemini", "openrouter"
	PromptPrice     float64 // cost per prompt token (0 for local)
	CompletionPrice float64 // cost per completion token (0 for local)
}

// Endpoint describes where to send inference requests.
type Endpoint struct {
	SocketPath string // for local Docker/Ollama (unix socket or tcp://)
	BaseURL    string // for remote HTTPS providers
	APIKey     string // bearer token for authenticated providers
	Provider   string // provider name for routing (e.g. "anthropic", "openai")
	Region     string // AWS region for Bedrock
}

// IsRemote returns true if this endpoint targets a remote provider.
func (e Endpoint) IsRemote() bool { return e.BaseURL != "" || e.Provider != "" }

// ToolCall represents a completed tool call from the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the function name and arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// DeltaToolCall is a streaming fragment of a tool call.
type DeltaToolCall struct {
	Index    int            `json:"index"`
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"type,omitempty"`
	Function *DeltaFunction `json:"function,omitempty"`
}

// DeltaFunction is a streaming fragment of a function call.
type DeltaFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition describes a function the model can call.
type FunctionDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// StreamEvent is the unified event type emitted by the streaming pipeline.
type StreamEvent struct {
	Token          string           // text token from the model
	Error          string           // error message
	ToolStart      *ToolStartEvent  // tool execution starting
	ToolResult     *ToolResultEvent // tool execution completed
	Done           bool             // stream finished
	ContentReplace *string          // replaces accumulated streaming text (strips tool-call syntax)
	Usage          *UsageStats      // token usage from the API (present on Done)
	ToolsDisabled  bool             // signals that tools were dropped (model doesn't support them)
}

// ToolStartEvent signals the beginning of a tool execution.
type ToolStartEvent struct {
	CallID string
	Name   string
	Args   string
}

// ToolResultEvent carries the result of a tool execution.
type ToolResultEvent struct {
	CallID  string
	Name    string
	Content string
	IsError bool
}

// ContentPart represents a part of a multipart message (text or image).
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds an image URL (can be data: URI with base64 encoding).
type ImageURL struct {
	URL string `json:"url"`
}

// ChatMessage is a single message in the conversation.
type ChatMessage struct {
	Role         string        `json:"role"`
	Content      *string       `json:"content"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	Name         string        `json:"name,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
}

// NewChatMessage creates a ChatMessage with a non-nil content string.
func NewChatMessage(role, content string) ChatMessage {
	return ChatMessage{Role: role, Content: &content}
}

// NewMultipartMessage creates a ChatMessage with text and image parts.
func NewMultipartMessage(role, text string, images []ContentPart) ChatMessage {
	parts := []ContentPart{{Type: "text", Text: text}}
	parts = append(parts, images...)
	return ChatMessage{Role: role, ContentParts: parts, Content: &text}
}

// MarshalJSON customizes JSON encoding for ChatMessage.
// When ContentParts is non-empty, the "content" field is serialized as an array
// of content parts (OpenAI vision API format) instead of a plain string.
func (m ChatMessage) MarshalJSON() ([]byte, error) {
	type plain struct {
		Role       string      `json:"role"`
		Content    interface{} `json:"content"`
		Name       string      `json:"name,omitempty"`
		ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
		ToolCallID string      `json:"tool_call_id,omitempty"`
	}
	p := plain{
		Role:       m.Role,
		Name:       m.Name,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	}
	if len(m.ContentParts) > 0 {
		p.Content = m.ContentParts
	} else if m.Content != nil {
		p.Content = *m.Content
	}
	return json.Marshal(p)
}

// ChatParams holds optional model parameters for inference.
type ChatParams struct {
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature"`
	TopP        *float64 `json:"top_p,omitempty"       yaml:"top_p"`
	MaxTokens   *int     `json:"max_tokens,omitempty"  yaml:"max_tokens"`
	TopK        *int     `json:"top_k,omitempty"        yaml:"top_k"`
	Stop        []string `json:"stop,omitempty"         yaml:"stop"`
}

// ChatRequest is the body sent to /v1/chat/completions.
type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []ChatMessage    `json:"messages"`
	Stream      bool             `json:"stream"`
	Temperature *float64         `json:"temperature,omitempty"`
	TopP        *float64         `json:"top_p,omitempty"`
	MaxTokens   *int             `json:"max_tokens,omitempty"`
	TopK        *int             `json:"top_k,omitempty"`
	Stop        []string         `json:"stop,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  interface{}      `json:"tool_choice,omitempty"`
}

// StreamChoice represents a single choice in a streaming chunk.
type StreamChoice struct {
	Delta struct {
		Content   string          `json:"content"`
		ToolCalls []DeltaToolCall `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// UsageStats holds token usage reported by the API.
type UsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk is one SSE frame from the streaming response.
type StreamChunk struct {
	Choices []StreamChoice `json:"choices"`
	Usage   *UsageStats    `json:"usage,omitempty"`
}
