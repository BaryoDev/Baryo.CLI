// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// DefaultSystemPrompt is the built-in system prompt shipped with the binary.
const DefaultSystemPrompt = "You are Baryo, a helpful AI assistant running locally via Docker Model Runner. Be concise, accurate, and helpful. When showing code, use fenced code blocks with the language specified."

// Config holds merged configuration from all sources.
type Config struct {
	Model        string `yaml:"model"`
	SocketPath   string `yaml:"socket_path"`
	SystemPrompt string `yaml:"system_prompt"`
}

// defaultSocketPath returns the platform-specific default socket path.
func defaultSocketPath() string {
	if p := os.Getenv("DOCKER_MODEL_SOCKET"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Containers", "com.docker.docker", "Data", "inference.sock")
	case "linux":
		return filepath.Join(home, ".docker", "desktop", "inference.sock")
	default:
		return filepath.Join(home, ".docker", "desktop", "inference.sock")
	}
}

// Load reads configuration with the following precedence (highest wins):
//
//	env vars > .baryo/config.yaml (project) > ~/.baryo/config.yaml (user) > defaults
func Load() Config {
	cfg := Config{
		SocketPath:   defaultSocketPath(),
		SystemPrompt: DefaultSystemPrompt,
	}

	// User-level config: ~/.baryo/config.yaml
	if home, err := os.UserHomeDir(); err == nil {
		loadFile(filepath.Join(home, ".baryo", "config.yaml"), &cfg)
	}

	// Project-level config: .baryo/config.yaml (overrides user)
	loadFile(filepath.Join(".baryo", "config.yaml"), &cfg)

	// Environment variable overrides
	applyEnv(&cfg)

	return cfg
}

// loadFile reads a YAML config file and merges non-zero values into cfg.
func loadFile(path string, cfg *Config) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist or unreadable — skip
	}

	var file Config
	if err := yaml.Unmarshal(data, &file); err != nil {
		return // malformed YAML — skip silently
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
}

// ApplyCLI merges CLI flag values on top of config (highest precedence).
// Only non-empty values are applied.
func (c *Config) ApplyCLI(model, systemPrompt string) {
	if model != "" {
		c.Model = model
	}
	if systemPrompt != "" {
		c.SystemPrompt = systemPrompt
	}
}
