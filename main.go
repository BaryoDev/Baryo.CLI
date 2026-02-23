// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/arnelirobles/baryo-cli/internal/cli"
	"github.com/arnelirobles/baryo-cli/internal/docker"
	"github.com/arnelirobles/baryo-cli/internal/tui"
)

func main() {
	cfg := cli.Parse()

	switch cfg.Mode() {
	case cli.ModeVersion:
		cli.PrintVersion()
		return

	case cli.ModeHelp:
		cli.PrintHelp()
		return

	case cli.ModePrint:
		model, err := resolveModel(cfg.Model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := cli.RunPrint(model, cfg.FullPrompt()); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return

	case cli.ModeInteractive:
		var opts []tui.AppOption
		if cfg.Model != "" {
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
