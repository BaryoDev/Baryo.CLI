// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

// dockerModelRaw matches the actual JSON from `docker model list --json`.
type dockerModelRaw struct {
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

// DockerModel is the cleaned-up model info we use in the TUI.
type DockerModel struct {
	Name   string // e.g. "ai/mistral"
	Tag    string // full tag e.g. "docker.io/ai/mistral:latest"
	Params string // e.g. "7.25 B"
	Size   string // e.g. "4.07 GiB"
}

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

// ChatMessage is a single message in the conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// NewChatMessage creates a ChatMessage with a non-nil content string.
func NewChatMessage(role, content string) ChatMessage {
	return ChatMessage{Role: role, Content: &content}
}

// ChatParams holds optional model parameters for inference.
type ChatParams struct {
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature"`
	TopP        *float64 `json:"top_p,omitempty"       yaml:"top_p"`
	MaxTokens   *int     `json:"max_tokens,omitempty"  yaml:"max_tokens"`
}

// ChatRequest is the body sent to /v1/chat/completions.
type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []ChatMessage    `json:"messages"`
	Stream      bool             `json:"stream"`
	Temperature *float64         `json:"temperature,omitempty"`
	TopP        *float64         `json:"top_p,omitempty"`
	MaxTokens   *int             `json:"max_tokens,omitempty"`
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

// StreamChunk is one SSE frame from the streaming response.
type StreamChunk struct {
	Choices []StreamChoice `json:"choices"`
}
