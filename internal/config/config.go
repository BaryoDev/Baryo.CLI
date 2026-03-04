// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/mcp"
	"github.com/arnelirobles/baryo-cli/internal/tunnel"
	"gopkg.in/yaml.v3"
)

// DefaultSystemPrompt is the built-in system prompt shipped with the binary.
//
//go:embed prompts/system.md
var DefaultSystemPrompt string

// Config holds merged configuration from all sources.
type Config struct {
	Model          string            `yaml:"model"`
	SocketPath     string            `yaml:"socket_path"`
	SystemPrompt   string            `yaml:"system_prompt"`
	Params         llm.ChatParams `yaml:"params"`
	SSHTunnel      *tunnel.Config    `yaml:"ssh_tunnel"`
	SearchProvider   string            `yaml:"search_provider"`
	SearchAPIKey     string            `yaml:"search_api_key"`
	PermissionMode   string            `yaml:"permission_mode"` // "auto", "confirm", "suggest"
	GeminiAPIKey     string            `yaml:"gemini_api_key"`
	OpenRouterAPIKey string            `yaml:"openrouter_api_key"`
	OpenAIAPIKey     string            `yaml:"openai_api_key"`
	AnthropicAPIKey  string            `yaml:"anthropic_api_key"`
	ProviderKeys     map[string]string `yaml:"provider_keys"`
	MCPServers       []mcp.ServerConfig `yaml:"mcp_servers"`
	Rewrite          *bool              `yaml:"rewrite"`          // prompt rewrite pass (default true)
	MCPInReadOnly    *bool              `yaml:"mcp_in_read_only"` // allow MCP tools in read-only modes (default true)
	ExportPath       string             `yaml:"export_path"`      // default directory for /export output
	AutoLint         *bool              `yaml:"auto_lint"`        // run linter after code edits (default false)
	AutoTest         *bool              `yaml:"auto_test"`        // run tests after code edits (default false)
	LintCommand      string             `yaml:"lint_command"`     // custom lint command override
	TestCommand      string             `yaml:"test_command"`     // custom test command override
	Hooks            HooksConfig        `yaml:"hooks"`            // lifecycle hook commands
	AutoMode         []AutoModeEntry    `yaml:"auto_mode"`        // ordered models for auto-routing
	Notifications    *bool              `yaml:"notifications"`    // desktop notifications on completion (default false)
	SessionRetentionDays int            `yaml:"session_retention_days"` // auto-delete sessions older than N days (0 = keep all)
	Sandbox          *bool              `yaml:"sandbox"`          // run code in Docker sandbox (default false)
}

// HooksConfig holds shell commands that run on lifecycle events.
type HooksConfig struct {
	PreTool     string `yaml:"pre_tool"`
	PostTool    string `yaml:"post_tool"`
	OnError     string `yaml:"on_error"`
	OnCommit    string `yaml:"on_commit"`
	OnStreamEnd string `yaml:"on_stream_end"`
	OnSearch    string `yaml:"on_search"`
}

// HasAny returns true if any hook is configured.
func (h HooksConfig) HasAny() bool {
	return h.PreTool != "" || h.PostTool != "" || h.OnError != "" ||
		h.OnCommit != "" || h.OnStreamEnd != "" || h.OnSearch != ""
}

// AutoModeEntry maps a model tag to a capability tier for auto-routing.
type AutoModeEntry struct {
	Model string `yaml:"model"` // model tag (e.g. "gemini/gemini-2.0-flash")
	Tier  string `yaml:"tier"`  // "fast", "normal", "strong"
}

// RewriteEnabled returns whether the prompt rewrite pass is enabled.
// Defaults to true when not explicitly set.
func (c *Config) RewriteEnabled() bool {
	if c.Rewrite == nil {
		return true
	}
	return *c.Rewrite
}

// MCPInReadOnlyEnabled returns whether MCP tools are allowed in read-only modes.
// Defaults to true when not explicitly set.
func (c *Config) MCPInReadOnlyEnabled() bool {
	if c.MCPInReadOnly == nil {
		return true
	}
	return *c.MCPInReadOnly
}

// AutoLintEnabled returns whether auto-lint after code edits is enabled.
// Defaults to false when not explicitly set.
func (c *Config) AutoLintEnabled() bool {
	if c.AutoLint == nil {
		return false
	}
	return *c.AutoLint
}

// AutoTestEnabled returns whether auto-test after code edits is enabled.
// Defaults to false when not explicitly set.
func (c *Config) AutoTestEnabled() bool {
	if c.AutoTest == nil {
		return false
	}
	return *c.AutoTest
}

// NotificationsEnabled returns whether desktop notifications are enabled.
// Defaults to false when not explicitly set.
func (c *Config) NotificationsEnabled() bool {
	if c.Notifications == nil {
		return false
	}
	return *c.Notifications
}

// SandboxEnabled returns whether sandboxed code execution is enabled.
// Defaults to false when not explicitly set.
func (c *Config) SandboxEnabled() bool {
	if c.Sandbox == nil {
		return false
	}
	return *c.Sandbox
}

// defaultSocketPath returns the platform-specific default socket path.
// It checks environment variables first, then probes known locations.
func defaultSocketPath() string {
	if p := os.Getenv("DOCKER_MODEL_SOCKET"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Containers", "com.llm.docker", "Data", "inference.sock")
	case "linux":
		return probeLinuxSocket(home)
	case "windows":
		return `//./pipe/docker_model_runner`
	default:
		return filepath.Join(home, ".docker", "desktop", "inference.sock")
	}
}

// probeLinuxSocket checks known Linux socket paths and returns the first that exists.
func probeLinuxSocket(home string) string {
	candidates := []string{
		filepath.Join(home, ".docker", "desktop", "inference.sock"),
		filepath.Join(home, ".docker", "inference.sock"),
		"/var/run/docker/inference.sock",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fallback to the most common path even if it doesn't exist yet
	return candidates[0]
}

// Load reads configuration with the following precedence (highest wins):
//
//	env vars > .baryo/config.yaml (project) > ~/.baryo/config.yaml (user) > defaults
func Load() Config {
	cfg := Config{
		SocketPath:     defaultSocketPath(),
		SystemPrompt:   DefaultSystemPrompt,
		PermissionMode: "confirm",
	}

	// User-level config: ~/.baryo/config.yaml
	if home, err := os.UserHomeDir(); err == nil {
		loadFile(filepath.Join(home, ".baryo", "config.yaml"), &cfg)
	}

	// Project-level config: .baryo/config.yaml (overrides user)
	loadFile(filepath.Join(".baryo", "config.yaml"), &cfg)

	// Environment variable overrides
	applyEnv(&cfg)

	// Build unified provider keys map from all sources.
	cfg.BuildProviderKeys()

	return cfg
}

// BuildProviderKeys merges individual API key fields and env vars into
// the unified ProviderKeys map. Precedence: env vars > provider_keys yaml > individual fields.
func (c *Config) BuildProviderKeys() {
	if c.ProviderKeys == nil {
		c.ProviderKeys = make(map[string]string)
	}
	// Individual fields are lowest precedence — only set if not already present.
	legacy := map[string]string{
		"gemini":     c.GeminiAPIKey,
		"openrouter": c.OpenRouterAPIKey,
		"openai":     c.OpenAIAPIKey,
		"anthropic":  c.AnthropicAPIKey,
	}
	for provider, key := range legacy {
		if key != "" {
			if _, exists := c.ProviderKeys[provider]; !exists {
				c.ProviderKeys[provider] = key
			}
		}
	}
}

// loadFile reads a YAML config file and merges non-zero values into cfg.
func loadFile(path string, cfg *Config) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist or unreadable — skip
	}

	var file Config
	if err := yaml.Unmarshal(data, &file); err != nil {
		fmt.Fprintf(os.Stderr, "warning: malformed config %s: %v\n", path, err)
		return
	}

	if file.Model != "" {
		cfg.Model = file.Model
	}
	if file.SocketPath != "" {
		cfg.SocketPath = file.SocketPath
	}
	if file.SystemPrompt != "" {
		cfg.SystemPrompt = file.SystemPrompt
	}
	if file.Params.Temperature != nil {
		cfg.Params.Temperature = file.Params.Temperature
	}
	if file.Params.TopP != nil {
		cfg.Params.TopP = file.Params.TopP
	}
	if file.Params.MaxTokens != nil {
		cfg.Params.MaxTokens = file.Params.MaxTokens
	}
	if file.SSHTunnel != nil && file.SSHTunnel.IsConfigured() {
		cfg.SSHTunnel = file.SSHTunnel
	}
	if file.SearchProvider != "" {
		cfg.SearchProvider = file.SearchProvider
	}
	if file.SearchAPIKey != "" {
		cfg.SearchAPIKey = file.SearchAPIKey
	}
	if file.PermissionMode != "" {
		cfg.PermissionMode = file.PermissionMode
	}
	if file.GeminiAPIKey != "" {
		cfg.GeminiAPIKey = file.GeminiAPIKey
	}
	if file.OpenRouterAPIKey != "" {
		cfg.OpenRouterAPIKey = file.OpenRouterAPIKey
	}
	if file.OpenAIAPIKey != "" {
		cfg.OpenAIAPIKey = file.OpenAIAPIKey
	}
	if file.AnthropicAPIKey != "" {
		cfg.AnthropicAPIKey = file.AnthropicAPIKey
	}
	if len(file.ProviderKeys) > 0 {
		if cfg.ProviderKeys == nil {
			cfg.ProviderKeys = make(map[string]string)
		}
		for k, v := range file.ProviderKeys {
			cfg.ProviderKeys[k] = v
		}
	}
	if file.Rewrite != nil {
		cfg.Rewrite = file.Rewrite
	}
	if file.MCPInReadOnly != nil {
		cfg.MCPInReadOnly = file.MCPInReadOnly
	}
	if len(file.MCPServers) > 0 {
		cfg.MCPServers = file.MCPServers
	}
	if file.ExportPath != "" {
		cfg.ExportPath = file.ExportPath
	}
	if file.AutoLint != nil {
		cfg.AutoLint = file.AutoLint
	}
	if file.AutoTest != nil {
		cfg.AutoTest = file.AutoTest
	}
	if file.LintCommand != "" {
		cfg.LintCommand = file.LintCommand
	}
	if file.TestCommand != "" {
		cfg.TestCommand = file.TestCommand
	}
	// Hooks: merge each field independently
	if file.Hooks.PreTool != "" {
		cfg.Hooks.PreTool = file.Hooks.PreTool
	}
	if file.Hooks.PostTool != "" {
		cfg.Hooks.PostTool = file.Hooks.PostTool
	}
	if file.Hooks.OnError != "" {
		cfg.Hooks.OnError = file.Hooks.OnError
	}
	if file.Hooks.OnCommit != "" {
		cfg.Hooks.OnCommit = file.Hooks.OnCommit
	}
	if file.Hooks.OnStreamEnd != "" {
		cfg.Hooks.OnStreamEnd = file.Hooks.OnStreamEnd
	}
	if file.Hooks.OnSearch != "" {
		cfg.Hooks.OnSearch = file.Hooks.OnSearch
	}
	// Auto mode: replace entire list if present
	if len(file.AutoMode) > 0 {
		cfg.AutoMode = file.AutoMode
	}
	if file.Notifications != nil {
		cfg.Notifications = file.Notifications
	}
	if file.SessionRetentionDays > 0 {
		cfg.SessionRetentionDays = file.SessionRetentionDays
	}
	if file.Sandbox != nil {
		cfg.Sandbox = file.Sandbox
	}
}

// applyEnv overrides config values from environment variables.
func applyEnv(cfg *Config) {
	if v := os.Getenv("BARYO_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("BARYO_SOCKET"); v != "" {
		cfg.SocketPath = v
	}
	if v := os.Getenv("BARYO_SYSTEM_PROMPT"); v != "" {
		cfg.SystemPrompt = v
	}
	if v := os.Getenv("BARYO_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Params.Temperature = &f
		}
	}
	if v := os.Getenv("BARYO_TOP_P"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Params.TopP = &f
		}
	}
	if v := os.Getenv("BARYO_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Params.MaxTokens = &n
		}
	}
	if v := os.Getenv("BARYO_SEARCH_PROVIDER"); v != "" {
		cfg.SearchProvider = v
	}
	if v := os.Getenv("BARYO_SEARCH_API_KEY"); v != "" {
		cfg.SearchAPIKey = v
	}
	if v := os.Getenv("BARYO_PERMISSION_MODE"); v != "" {
		cfg.PermissionMode = v
	}
	if v := os.Getenv("BARYO_REWRITE"); v != "" {
		b := v == "true" || v == "1" || v == "yes"
		cfg.Rewrite = &b
	}
	if v := os.Getenv("BARYO_MCP_IN_READ_ONLY"); v != "" {
		b := v == "true" || v == "1" || v == "yes"
		cfg.MCPInReadOnly = &b
	}
	if v := os.Getenv("BARYO_EXPORT_PATH"); v != "" {
		cfg.ExportPath = v
	}
	if v := os.Getenv("BARYO_AUTO_LINT"); v != "" {
		b := v == "true" || v == "1" || v == "yes"
		cfg.AutoLint = &b
	}
	if v := os.Getenv("BARYO_AUTO_TEST"); v != "" {
		b := v == "true" || v == "1" || v == "yes"
		cfg.AutoTest = &b
	}
	if v := os.Getenv("BARYO_LINT_COMMAND"); v != "" {
		cfg.LintCommand = v
	}
	if v := os.Getenv("BARYO_TEST_COMMAND"); v != "" {
		cfg.TestCommand = v
	}
	// Hook env vars
	if v := os.Getenv("BARYO_HOOK_PRE_TOOL"); v != "" {
		cfg.Hooks.PreTool = v
	}
	if v := os.Getenv("BARYO_HOOK_POST_TOOL"); v != "" {
		cfg.Hooks.PostTool = v
	}
	if v := os.Getenv("BARYO_HOOK_ON_ERROR"); v != "" {
		cfg.Hooks.OnError = v
	}
	if v := os.Getenv("BARYO_HOOK_ON_COMMIT"); v != "" {
		cfg.Hooks.OnCommit = v
	}
	if v := os.Getenv("BARYO_HOOK_ON_STREAM_END"); v != "" {
		cfg.Hooks.OnStreamEnd = v
	}
	if v := os.Getenv("BARYO_HOOK_ON_SEARCH"); v != "" {
		cfg.Hooks.OnSearch = v
	}
	if v := os.Getenv("BARYO_NOTIFICATIONS"); v != "" {
		b := v == "true" || v == "1" || v == "yes"
		cfg.Notifications = &b
	}
	if v := os.Getenv("BARYO_SESSION_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.SessionRetentionDays = n
		}
	}
	if v := os.Getenv("BARYO_SANDBOX"); v != "" {
		b := v == "true" || v == "1" || v == "yes"
		cfg.Sandbox = &b
	}
	// Legacy env vars — also write into ProviderKeys so env always wins over YAML.
	legacyEnvVars := map[string]string{
		"gemini":     "BARYO_GEMINI_API_KEY",
		"openrouter": "BARYO_OPENROUTER_API_KEY",
		"openai":     "BARYO_OPENAI_API_KEY",
		"anthropic":  "BARYO_ANTHROPIC_API_KEY",
	}
	for provider, envKey := range legacyEnvVars {
		if v := os.Getenv(envKey); v != "" {
			switch provider {
			case "gemini":
				cfg.GeminiAPIKey = v
			case "openrouter":
				cfg.OpenRouterAPIKey = v
			case "openai":
				cfg.OpenAIAPIKey = v
			case "anthropic":
				cfg.AnthropicAPIKey = v
			}
			if cfg.ProviderKeys == nil {
				cfg.ProviderKeys = make(map[string]string)
			}
			cfg.ProviderKeys[provider] = v
		}
	}

	// New provider env vars (written directly into ProviderKeys map).
	providerEnvVars := map[string]string{
		"groq":       "BARYO_GROQ_API_KEY",
		"mistral":    "BARYO_MISTRAL_API_KEY",
		"together":   "BARYO_TOGETHER_API_KEY",
		"fireworks":  "BARYO_FIREWORKS_API_KEY",
		"deepseek":   "BARYO_DEEPSEEK_API_KEY",
		"xai":        "BARYO_XAI_API_KEY",
		"cerebras":   "BARYO_CEREBRAS_API_KEY",
		"perplexity": "BARYO_PERPLEXITY_API_KEY",
		"sambanova":  "BARYO_SAMBANOVA_API_KEY",
		"cohere":     "BARYO_COHERE_API_KEY",
		"bedrock":      "BARYO_BEDROCK_REGION",
		"huggingface": "BARYO_HUGGINGFACE_API_KEY",
		"github":      "BARYO_GITHUB_TOKEN",
		"ollama":      "BARYO_OLLAMA_API_KEY",
	}
	for provider, envKey := range providerEnvVars {
		if v := os.Getenv(envKey); v != "" {
			if cfg.ProviderKeys == nil {
				cfg.ProviderKeys = make(map[string]string)
			}
			cfg.ProviderKeys[provider] = v
		}
	}
}

// ApplyCLI merges CLI flag values on top of config (highest precedence).
// Only non-empty values are applied. If yolo is true, PermissionMode is set to "auto".
func (c *Config) ApplyCLI(model, systemPrompt, tunnelFlag string, params llm.ChatParams, yolo bool) {
	if model != "" {
		c.Model = model
	}
	if systemPrompt != "" {
		c.SystemPrompt = systemPrompt
	}
	if params.Temperature != nil {
		c.Params.Temperature = params.Temperature
	}
	if params.TopP != nil {
		c.Params.TopP = params.TopP
	}
	if params.MaxTokens != nil {
		c.Params.MaxTokens = params.MaxTokens
	}
	if tunnelFlag != "" {
		c.SSHTunnel = parseTunnelFlag(tunnelFlag)
	}
	if yolo {
		c.PermissionMode = "auto"
	}
}

// parseTunnelFlag parses a --tunnel flag value like "user@host" or "user@host:ssh_port"
// into a tunnel.Config with sensible defaults.
func parseTunnelFlag(val string) *tunnel.Config {
	cfg := &tunnel.Config{
		RemotePort: 11434,
		LocalPort:  11434,
		SSHPort:    22,
	}

	// Split user@host:port
	parts := strings.SplitN(val, "@", 2)
	if len(parts) == 2 {
		cfg.User = parts[0]
		cfg.Host = parts[1]
	} else {
		cfg.Host = val
	}

	// Check for :port suffix on host (SSH port override)
	if h, p, found := strings.Cut(cfg.Host, ":"); found {
		if port, err := strconv.Atoi(p); err == nil {
			cfg.Host = h
			cfg.SSHPort = port
		}
	}

	return cfg
}
