// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arnelirobles/baryo-cli/internal/cli"
	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/doctor"
	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/logger"
	"github.com/arnelirobles/baryo-cli/internal/mcp"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/tools"
	"github.com/arnelirobles/baryo-cli/internal/tui"
	"github.com/arnelirobles/baryo-cli/internal/tunnel"
	"github.com/arnelirobles/baryo-cli/internal/worktree"
)

func main() {
	flags := cli.Parse()

	if flags.Debug {
		home, _ := os.UserHomeDir()
		logPath := filepath.Join(home, ".baryo", "debug.log")
		if err := logger.Init(logPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot open debug log: %v\n", err)
		} else {
			defer logger.Close()
			logger.Info("baryo starting", "version", cli.Version)
		}
	}

	switch flags.Mode() {
	case cli.ModeVersion:
		cli.PrintVersion()
		return
	case cli.ModeHelp:
		cli.PrintHelp()
		return
	case cli.ModeDoctor:
		cfg := config.Load()
		cfg.ApplyCLI("", "", flags.Tunnel, llm.ChatParams{}, flags.Yolo)
		tun := startTunnel(&cfg)
		if tun != nil {
			defer tun.Close()
		}
		fmt.Println("Baryo — diagnostic check")
		results := doctor.RunChecks(cfg.SocketPath)
		fmt.Print(doctor.FormatResults(results))
		if doctor.AllPassed(results) {
			fmt.Println("\nAll checks passed. You're ready to go!")
		} else {
			os.Exit(1)
		}
		return
	case cli.ModeCompletion:
		script, err := cli.GenerateCompletion(flags.Completion)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(script)
		return
	}

	// Create git worktree if requested
	var wt *worktree.Worktree
	if flags.Worktree {
		var err error
		wt, err = worktree.Create("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating worktree: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Working in worktree: %s (branch: %s)\n", wt.Path, wt.Branch)
		if err := os.Chdir(wt.Path); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot chdir to worktree: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			if wt.HasChanges() {
				fmt.Fprintf(os.Stderr, "\nWorktree %s has uncommitted changes on branch %s\n", wt.Path, wt.Branch)
				fmt.Fprintf(os.Stderr, "Merge with: git merge %s\n", wt.Branch)
			} else {
				wt.Remove()
			}
		}()
	}

	// Load config and apply CLI flag overrides
	cfg := config.Load()
	cfg.ApplyCLI(flags.Model, flags.SystemPrompt, flags.Tunnel, flags.Params, flags.Yolo)

	// Start SSH tunnel if configured
	tun := startTunnel(&cfg)
	if tun != nil {
		defer tun.Close()
	}

	// Load BARYO.md and skills.md project instructions
	if instructions := config.LoadProjectInstructions(); instructions != "" {
		cfg.SystemPrompt = cfg.SystemPrompt + "\n\n<project-context>\n" + instructions + "\n</project-context>"
	}

	// Load saved memories (passed separately for prominent injection)
	var memoriesPrompt string
	if memories := config.LoadMemories(); len(memories) > 0 {
		memoriesPrompt = config.FormatMemoriesForPrompt(memories)
	}

	// Enable sandbox if configured or CLI flag set
	if cfg.SandboxEnabled() || flags.Sandbox {
		tools.EnableSandbox()
	}

	// Clean old sessions if retention is configured.
	if cfg.SessionRetentionDays > 0 {
		if deleted, _ := session.CleanOld(cfg.SessionRetentionDays); deleted > 0 {
			logger.Debug("session cleanup", "deleted", deleted, "retention_days", cfg.SessionRetentionDays)
		}
	}

	// Run startup health checks unless skipped or using a cloud provider model.
	skipDoctor := flags.SkipChecks
	if !skipDoctor && cfg.Model != "" {
		if pm, ok := tryProviderModel(&cfg); ok && isProviderModel(pm) {
			skipDoctor = true
		}
	}
	if !skipDoctor {
		results := doctor.RunChecks(cfg.SocketPath)
		if !doctor.AllPassed(results) {
			// If cloud providers are configured, just warn instead of exiting.
			hasProviders := false
			for _, key := range cfg.ProviderKeys {
				if key != "" {
					hasProviders = true
					break
				}
			}
			if hasProviders {
				fmt.Fprintf(os.Stderr, "Baryo — local inference not available (cloud providers will still work)\n")
			} else if doctor.IsOllamaRunning() {
				// Ollama is running — check if it has models.
				if models := llm.ListLocalOllama(); len(models) > 0 {
					cfg.SocketPath = "tcp://localhost:11434"
					fmt.Fprintf(os.Stderr, "Baryo — Docker not available, using Ollama (%d model(s))\n", len(models))
				} else {
					fmt.Fprintf(os.Stderr, "Baryo — Ollama is running but has no models\n\n")
					fmt.Fprintf(os.Stderr, "  Pull a model to get started:\n\n")
					fmt.Fprintf(os.Stderr, "    ollama pull qwen3:0.6b\n\n")
					os.Exit(1)
				}
			} else if doctor.HasOllama() {
				fmt.Fprintf(os.Stderr, "Baryo — Ollama is installed but not running\n\n")
				fmt.Fprintf(os.Stderr, "  Start it with: ollama serve\n\n")
				os.Exit(1)
			} else {
				fmt.Fprintf(os.Stderr, "Baryo — no local inference available\n\n")
				fmt.Fprint(os.Stderr, doctor.FormatResults(results))
				fmt.Fprintf(os.Stderr, "\nInstall Ollama (easiest option):\n")
				fmt.Fprintf(os.Stderr, "  macOS:  brew install ollama\n")
				fmt.Fprintf(os.Stderr, "  Linux:  curl -fsSL https://ollama.com/install.sh | sh\n\n")
				fmt.Fprintf(os.Stderr, "Then: ollama serve && ollama pull qwen3:0.6b\n\n")
				fmt.Fprintf(os.Stderr, "Or run 'baryo doctor' for full diagnostics or use --skip-checks to bypass.\n")
				os.Exit(1)
			}
		}
	}

	switch flags.Mode() {
	case cli.ModePrint:
		model, err := resolveModel(&cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(2)
		}
		ep := endpointForModel(cfg, model)

		// Inject memories into system prompt for headless mode.
		systemPrompt := cfg.SystemPrompt
		if memoriesPrompt != "" {
			systemPrompt = systemPrompt + "\n\n" + memoriesPrompt
		}

		// Warn when tools are enabled but permission mode blocks destructive ones.
		enableTools := !flags.NoTools
		if enableTools && cfg.PermissionMode != "auto" {
			fmt.Fprintf(os.Stderr, "warning: destructive tools require --yolo in headless mode\n")
		}

		// Start MCP servers only when tools are enabled.
		var mcpMgr *mcp.Manager
		if enableTools && len(cfg.MCPServers) > 0 {
			mcpMgr = mcp.NewManager()
			errs := mcpMgr.Start(context.Background(), cfg.MCPServers)
			for _, err := range errs {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
			defer mcpMgr.Close()
		}

		// Load and format strategy JSON if --strategy flag is provided.
		var strategyInput string
		if flags.Strategy != "" {
			si, err := cli.LoadStrategyFile(flags.Strategy)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(2)
			}
			strategyInput = si
		}

		prompt := flags.FullPrompt()
		if prompt == "" && strategyInput != "" {
			prompt = "Analyze the strategy input above and find optimal steps."
		}

		printOpts := cli.PrintOptions{
			Endpoint:       ep,
			SystemPrompt:   systemPrompt,
			Model:          model,
			Prompt:         prompt,
			Params:         cfg.Params,
			PermissionMode: cfg.PermissionMode,
			MaxTurns:       flags.MaxTurns,
			OutputFormat:   flags.Output,
			EnableTools:    enableTools,
			MCPManager:     mcpMgr,
			StrategyInput:  strategyInput,
			SearchProvider: cfg.SearchProvider,
			SearchAPIKey:   cfg.SearchAPIKey,
		}
		os.Exit(cli.RunPrint(printOpts))
		return

	case cli.ModeInteractive:
		opts := []tui.AppOption{
			tui.WithSocketPath(cfg.SocketPath),
			tui.WithSystemPrompt(cfg.SystemPrompt),
			tui.WithMemories(memoriesPrompt),
			tui.WithParams(cfg.Params),
			tui.WithSearchConfig(cfg.SearchProvider, cfg.SearchAPIKey),
			tui.WithPermissionMode(cfg.PermissionMode),
			tui.WithProviderKeys(cfg.ProviderKeys),
			tui.WithRewrite(cfg.RewriteEnabled()),
			tui.WithMCPInReadOnly(cfg.MCPInReadOnlyEnabled()),
			tui.WithExportPath(cfg.ExportPath),
			tui.WithAutoFix(tui.AutoFixConfig{
				AutoLint:    cfg.AutoLintEnabled(),
				AutoTest:    cfg.AutoTestEnabled(),
				LintCommand: cfg.LintCommand,
				TestCommand: cfg.TestCommand,
			}),
		}

		if cfg.NotificationsEnabled() {
			opts = append(opts, tui.WithNotifications(true))
		}

		if cfg.SandboxEnabled() {
			opts = append(opts, tui.WithSandbox(true))
		}

		if wt != nil {
			opts = append(opts, tui.WithWorktree(wt.Branch))
		}

		if cfg.Hooks.HasAny() {
			opts = append(opts, tui.WithHooks(cfg.Hooks))
		}

		if len(cfg.AutoMode) > 0 {
			opts = append(opts, tui.WithAutoMode(buildAutoModeConfig(cfg.AutoMode)))
		}

		if len(cfg.MCPServers) > 0 {
			opts = append(opts, tui.WithMCPConfigs(cfg.MCPServers))
		}

		// Handle session resume flags
		if flags.ResumeID != "" {
			sess, err := session.Load(flags.ResumeID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			opts = append(opts, tui.WithSession(sess))
		} else if flags.Resume {
			summaries, err := session.List()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			opts = append(opts, tui.WithSessionList(summaries))
		} else if flags.Continue {
			cwd, _ := os.Getwd()
			sess, err := session.LatestForDir(cwd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			opts = append(opts, tui.WithSession(sess))
		} else if cfg.Model != "" {
			model, err := resolveModel(&cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			opts = append(opts, tui.WithPreselectedModel(model))
		}

		p := tea.NewProgram(tui.NewApp(opts...), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

// startTunnel starts an SSH tunnel if configured and overrides SocketPath.
// Returns nil if no tunnel is configured or the port is already open.
func startTunnel(cfg *config.Config) *tunnel.Tunnel {
	if !cfg.SSHTunnel.IsConfigured() {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Starting SSH tunnel to %s@%s...\n", cfg.SSHTunnel.User, cfg.SSHTunnel.Host)
	tun, err := tunnel.Start(cfg.SSHTunnel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cfg.SocketPath = cfg.SSHTunnel.LocalAddr()
	if tun != nil {
		fmt.Fprintf(os.Stderr, "SSH tunnel active → %s\n", cfg.SocketPath)
	} else {
		fmt.Fprintf(os.Stderr, "Port already open → %s\n", cfg.SocketPath)
	}
	return tun
}

// resolveModel lists available models and matches the query.
// For TCP connections, it queries the remote server's API for available models.
// Also checks provider models when no local match is found.
func resolveModel(cfg *config.Config) (llm.Model, error) {
	// Try provider models first if model name has a known prefix.
	if cfg.Model != "" {
		if pm, ok := tryProviderModel(cfg); ok {
			return pm, nil
		}
	}

	if llm.IsRemoteSocket(cfg.SocketPath) {
		models, err := llm.ListRemoteModels(cfg.SocketPath)
		if err != nil {
			if cfg.Model != "" {
				return llm.Model{Name: cfg.Model, Tag: cfg.Model}, nil
			}
			return llm.Model{}, fmt.Errorf("cannot list remote models: %w", err)
		}
		if len(models) == 0 {
			return llm.Model{}, fmt.Errorf("no models available on the remote server")
		}
		if cfg.Model == "" {
			return models[0], nil
		}
		return cli.MatchModel(cfg.Model, models)
	}

	models, err := llm.ListModels()
	if err != nil {
		// Docker not available — still try Ollama.
		models = nil
	}
	// Also include local Ollama models.
	if om := llm.ListLocalOllama(); len(om) > 0 {
		models = append(models, om...)
	}
	if len(models) == 0 {
		return llm.Model{}, fmt.Errorf("no models available — pull a model with: docker model pull <name> or ollama pull <name>")
	}
	if cfg.Model == "" {
		return models[0], nil
	}
	return cli.MatchModel(cfg.Model, models)
}

// tryProviderModel checks if the model name matches a known provider prefix
// and returns the model directly without listing.
func tryProviderModel(cfg *config.Config) (llm.Model, bool) {
	provider := llm.DetectProvider(cfg.Model)
	if provider == "" {
		return llm.Model{}, false
	}
	key, ok := cfg.ProviderKeys[provider]
	if !ok || key == "" {
		return llm.Model{}, false
	}
	p := llm.LookupPricing(provider, cfg.Model)
	dm := llm.Model{
		Name:            cfg.Model,
		Tag:             cfg.Model,
		Provider:        provider,
		PromptPrice:     p.PromptPrice,
		CompletionPrice: p.CompletionPrice,
	}
	return dm, true
}

// endpointForModel returns the appropriate endpoint for a model.
func endpointForModel(cfg config.Config, model llm.Model) llm.Endpoint {
	if model.Provider == "ollama-local" {
		return llm.LocalEndpoint("tcp://localhost:11434")
	}
	if model.Provider != "" {
		if key, ok := cfg.ProviderKeys[model.Provider]; ok {
			return llm.ProviderEndpoint(model.Provider, key)
		}
	}
	return llm.LocalEndpoint(cfg.SocketPath)
}

// isProviderModel returns true if the model is a cloud provider model.
func isProviderModel(model llm.Model) bool {
	return model.Provider != ""
}

// buildAutoModeConfig converts config entries into an AutoModeConfig.
func buildAutoModeConfig(entries []config.AutoModeEntry) tui.AutoModeConfig {
	cfg := tui.AutoModeConfig{Enabled: true}
	for _, e := range entries {
		cfg.Models = append(cfg.Models, tui.AutoModeModel{
			Tag:  e.Model,
			Tier: tui.ParseTier(e.Tier),
		})
	}
	return cfg
}

