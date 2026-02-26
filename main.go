// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/arnelirobles/baryo-cli/internal/cli"
	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/doctor"
	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/tui"
	"github.com/arnelirobles/baryo-cli/internal/tunnel"
)

func main() {
	flags := cli.Parse()

	switch flags.Mode() {
	case cli.ModeVersion:
		cli.PrintVersion()
		return
	case cli.ModeHelp:
		cli.PrintHelp()
		return
	case cli.ModeDoctor:
		cfg := config.Load()
		cfg.ApplyCLI("", "", flags.Tunnel, docker.ChatParams{})
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
	cfg.ApplyCLI(flags.Model, flags.SystemPrompt, flags.Tunnel, flags.Params)

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

	// Run startup health checks unless skipped
	if !flags.SkipChecks {
		results := doctor.RunChecks(cfg.SocketPath)
		if !doctor.AllPassed(results) {
			fmt.Fprintf(os.Stderr, "Baryo — startup check failed\n\n")
			fmt.Fprint(os.Stderr, doctor.FormatResults(results))
			fmt.Fprintf(os.Stderr, "\nRun 'baryo doctor' for full diagnostics or use --skip-checks to bypass.\n")
			os.Exit(1)
		}
	}

	switch flags.Mode() {
	case cli.ModePrint:
		model, err := resolveModel(&cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := cli.RunPrint(cfg.SocketPath, cfg.SystemPrompt, model, flags.FullPrompt(), cfg.Params); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return

	case cli.ModeInteractive:
		opts := []tui.AppOption{
			tui.WithSocketPath(cfg.SocketPath),
			tui.WithSystemPrompt(cfg.SystemPrompt),
			tui.WithMemories(memoriesPrompt),
			tui.WithParams(cfg.Params),
			tui.WithSearchConfig(cfg.SearchProvider, cfg.SearchAPIKey),
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
func resolveModel(cfg *config.Config) (docker.DockerModel, error) {
	if isRemoteSocket(cfg.SocketPath) {
		models, err := docker.ListRemoteModels(cfg.SocketPath)
		if err != nil {
			// Fallback: if we can't list models but have a model name, use it directly.
			if cfg.Model != "" {
				return docker.DockerModel{Name: cfg.Model, Tag: cfg.Model}, nil
			}
			return docker.DockerModel{}, fmt.Errorf("cannot list remote models: %w", err)
		}
		if len(models) == 0 {
			return docker.DockerModel{}, fmt.Errorf("no models available on the remote server")
		}
		if cfg.Model == "" {
			return models[0], nil
		}
		return cli.MatchModel(cfg.Model, models)
	}

	models, err := docker.ListModels()
	if err != nil {
		return docker.DockerModel{}, err
	}
	if len(models) == 0 {
		return docker.DockerModel{}, fmt.Errorf("no models available — pull a model with: docker model pull <name>")
	}
	if cfg.Model == "" {
		return models[0], nil
	}
	return cli.MatchModel(cfg.Model, models)
}

func isRemoteSocket(socketPath string) bool {
	if strings.HasPrefix(socketPath, "tcp://") {
		return true
	}
	// Bare host:port like "localhost:11434"
	_, _, err := net.SplitHostPort(socketPath)
	return err == nil
}
