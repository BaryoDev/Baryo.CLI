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

// ChatMessage is a single message in the conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the body sent to /v1/chat/completions.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// StreamChoice represents a single choice in a streaming chunk.
type StreamChoice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// StreamChunk is one SSE frame from the streaming response.
type StreamChunk struct {
	Choices []StreamChoice `json:"choices"`
}
