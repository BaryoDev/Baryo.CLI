// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/arnelirobles/baryo-cli/internal/cli"
	"github.com/arnelirobles/baryo-cli/internal/config"
	"github.com/arnelirobles/baryo-cli/internal/doctor"
	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/session"
	"github.com/arnelirobles/baryo-cli/internal/tui"
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
		fmt.Println("Baryo — diagnostic check\n")
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
	cfg.ApplyCLI(flags.Model, flags.SystemPrompt, flags.Params)

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
		model, err := resolveModel(cfg.Model)
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
			tui.WithParams(cfg.Params),
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
			model, err := resolveModel(cfg.Model)
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

// resolveModel lists available models and matches the query.
// If query is empty, the first available model is returned.
func resolveModel(query string) (docker.DockerModel, error) {
	models, err := docker.ListModels()
	if err != nil {
		return docker.DockerModel{}, err
	}
	if len(models) == 0 {
		return docker.DockerModel{}, fmt.Errorf("no models available — pull a model with: docker model pull <name>")
	}
	if query == "" {
		return models[0], nil
	}
	return cli.MatchModel(query, models)
}
