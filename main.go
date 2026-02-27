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
	"github.com/arnelirobles/baryo-cli/internal/tui"
	"github.com/arnelirobles/baryo-cli/internal/tunnel"
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
				fmt.Fprintf(os.Stderr, "Baryo — local Docker not available (cloud providers will still work)\n")
			} else {
				fmt.Fprintf(os.Stderr, "Baryo — startup check failed\n\n")
				fmt.Fprint(os.Stderr, doctor.FormatResults(results))
				fmt.Fprintf(os.Stderr, "\nRun 'baryo doctor' for full diagnostics or use --skip-checks to bypass.\n")
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

		printOpts := cli.PrintOptions{
			Endpoint:       ep,
			SystemPrompt:   systemPrompt,
			Model:          model,
			Prompt:         flags.FullPrompt(),
			Params:         cfg.Params,
			PermissionMode: cfg.PermissionMode,
			MaxTurns:       flags.MaxTurns,
			OutputFormat:   flags.Output,
			EnableTools:    enableTools,
			MCPManager:     mcpMgr,
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
		return llm.Model{}, err
	}
	if len(models) == 0 {
		return llm.Model{}, fmt.Errorf("no models available — pull a model with: docker model pull <name>")
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

