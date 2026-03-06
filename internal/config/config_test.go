package config

import (
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

func TestRewriteEnabled_Default(t *testing.T) {
	cfg := Config{}
	if !cfg.RewriteEnabled() {
		t.Error("RewriteEnabled should default to true")
	}
}

func TestRewriteEnabled_ExplicitTrue(t *testing.T) {
	b := true
	cfg := Config{Rewrite: &b}
	if !cfg.RewriteEnabled() {
		t.Error("RewriteEnabled should be true when set to true")
	}
}

func TestRewriteEnabled_ExplicitFalse(t *testing.T) {
	b := false
	cfg := Config{Rewrite: &b}
	if cfg.RewriteEnabled() {
		t.Error("RewriteEnabled should be false when set to false")
	}
}

func TestMCPInReadOnlyEnabled_Default(t *testing.T) {
	cfg := Config{}
	if !cfg.MCPInReadOnlyEnabled() {
		t.Error("MCPInReadOnlyEnabled should default to true")
	}
}

func TestAutoLintEnabled_Default(t *testing.T) {
	cfg := Config{}
	if cfg.AutoLintEnabled() {
		t.Error("AutoLintEnabled should default to false")
	}
}

func TestAutoTestEnabled_Default(t *testing.T) {
	cfg := Config{}
	if cfg.AutoTestEnabled() {
		t.Error("AutoTestEnabled should default to false")
	}
}

func TestNotificationsEnabled_Default(t *testing.T) {
	cfg := Config{}
	if cfg.NotificationsEnabled() {
		t.Error("NotificationsEnabled should default to false")
	}
}

func TestSandboxEnabled_Default(t *testing.T) {
	cfg := Config{}
	if cfg.SandboxEnabled() {
		t.Error("SandboxEnabled should default to false")
	}
}

func TestSandboxEnabled_ExplicitTrue(t *testing.T) {
	b := true
	cfg := Config{Sandbox: &b}
	if !cfg.SandboxEnabled() {
		t.Error("SandboxEnabled should be true when set to true")
	}
}

func TestShowThinkingEnabled_Default(t *testing.T) {
	cfg := Config{}
	if cfg.ShowThinkingEnabled() {
		t.Error("ShowThinkingEnabled should default to false")
	}
}

func TestShowThinkingEnabled_ExplicitTrue(t *testing.T) {
	b := true
	cfg := Config{ShowThinking: &b}
	if !cfg.ShowThinkingEnabled() {
		t.Error("ShowThinkingEnabled should be true when set to true")
	}
}

func TestShowThinkingEnabled_ExplicitFalse(t *testing.T) {
	b := false
	cfg := Config{ShowThinking: &b}
	if cfg.ShowThinkingEnabled() {
		t.Error("ShowThinkingEnabled should be false when set to false")
	}
}

func TestHooksConfig_HasAny(t *testing.T) {
	tests := []struct {
		name  string
		hooks HooksConfig
		want  bool
	}{
		{"empty", HooksConfig{}, false},
		{"pre_tool", HooksConfig{PreTool: "echo pre"}, true},
		{"post_tool", HooksConfig{PostTool: "echo post"}, true},
		{"on_error", HooksConfig{OnError: "echo err"}, true},
		{"on_commit", HooksConfig{OnCommit: "echo commit"}, true},
		{"on_stream_end", HooksConfig{OnStreamEnd: "echo done"}, true},
		{"on_search", HooksConfig{OnSearch: "echo search"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hooks.HasAny(); got != tt.want {
				t.Errorf("HasAny() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildProviderKeys(t *testing.T) {
	cfg := Config{
		GeminiAPIKey:     "gemini-key",
		OpenAIAPIKey:     "openai-key",
		AnthropicAPIKey:  "anthropic-key",
		OpenRouterAPIKey: "openrouter-key",
	}

	cfg.BuildProviderKeys()

	expected := map[string]string{
		"gemini":     "gemini-key",
		"openai":     "openai-key",
		"anthropic":  "anthropic-key",
		"openrouter": "openrouter-key",
	}

	for provider, key := range expected {
		if cfg.ProviderKeys[provider] != key {
			t.Errorf("ProviderKeys[%q] = %q, want %q", provider, cfg.ProviderKeys[provider], key)
		}
	}
}

func TestBuildProviderKeys_DoesNotOverrideExisting(t *testing.T) {
	cfg := Config{
		GeminiAPIKey: "old-key",
		ProviderKeys: map[string]string{
			"gemini": "yaml-key",
		},
	}

	cfg.BuildProviderKeys()

	if cfg.ProviderKeys["gemini"] != "yaml-key" {
		t.Errorf("ProviderKeys[gemini] = %q, want %q (should not override)", cfg.ProviderKeys["gemini"], "yaml-key")
	}
}

func TestApplyCLI(t *testing.T) {
	cfg := Config{
		Model:          "original-model",
		SystemPrompt:   "original-prompt",
		PermissionMode: "confirm",
	}

	temp := 0.5
	params := llm.ChatParams{Temperature: &temp}
	cfg.ApplyCLI("new-model", "new-prompt", "", params, false)

	if cfg.Model != "new-model" {
		t.Errorf("Model = %q, want %q", cfg.Model, "new-model")
	}
	if cfg.SystemPrompt != "new-prompt" {
		t.Errorf("SystemPrompt = %q, want %q", cfg.SystemPrompt, "new-prompt")
	}
	if cfg.Params.Temperature == nil || *cfg.Params.Temperature != 0.5 {
		t.Error("Temperature should be set to 0.5")
	}
	if cfg.PermissionMode != "confirm" {
		t.Error("PermissionMode should remain confirm when yolo=false")
	}
}

func TestApplyCLI_Yolo(t *testing.T) {
	cfg := Config{PermissionMode: "confirm"}
	cfg.ApplyCLI("", "", "", llm.ChatParams{}, true)
	if cfg.PermissionMode != "auto" {
		t.Errorf("PermissionMode = %q, want %q (yolo should set auto)", cfg.PermissionMode, "auto")
	}
}

func TestApplyCLI_EmptySkipsOverride(t *testing.T) {
	cfg := Config{
		Model:        "keep-this",
		SystemPrompt: "keep-this-too",
	}
	cfg.ApplyCLI("", "", "", llm.ChatParams{}, false)
	if cfg.Model != "keep-this" {
		t.Error("empty model should not override existing")
	}
	if cfg.SystemPrompt != "keep-this-too" {
		t.Error("empty system prompt should not override existing")
	}
}

func TestParseTunnelFlag(t *testing.T) {
	tests := []struct {
		input    string
		wantUser string
		wantHost string
		wantPort int
	}{
		{"user@example.com", "user", "example.com", 22},
		{"user@example.com:2222", "user", "example.com", 2222},
		{"example.com", "", "example.com", 22},
		{"example.com:2222", "", "example.com", 2222},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cfg := parseTunnelFlag(tt.input)
			if cfg.User != tt.wantUser {
				t.Errorf("User = %q, want %q", cfg.User, tt.wantUser)
			}
			if cfg.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", cfg.Host, tt.wantHost)
			}
			if cfg.SSHPort != tt.wantPort {
				t.Errorf("SSHPort = %d, want %d", cfg.SSHPort, tt.wantPort)
			}
			if cfg.RemotePort != 11434 {
				t.Errorf("RemotePort = %d, want 11434", cfg.RemotePort)
			}
			if cfg.LocalPort != 11434 {
				t.Errorf("LocalPort = %d, want 11434", cfg.LocalPort)
			}
		})
	}
}
