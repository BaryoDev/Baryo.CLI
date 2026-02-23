// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/docker"
)

type screen int

const (
	screenLoading screen = iota
	screenModelSelect
	screenChat
)

// AppModel is the top-level bubbletea model that drives screen transitions.
type AppModel struct {
	screen  screen
	spinner spinner.Model

	modelSelect ModelSelectModel
	chat        ChatModel

	err    error
	width  int
	height int
}

// NewApp creates the initial application model.
func NewApp() AppModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("205"))),
	)
	return AppModel{
		screen:  screenLoading,
		spinner: s,
	}
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, loadModels)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	switch m.screen {
	case screenLoading:
		return m.updateLoading(msg)
	case screenModelSelect:
		return m.updateModelSelect(msg)
	case screenChat:
		return m.updateChat(msg)
	}

	return m, nil
}

func (m AppModel) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ModelsLoadedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.screen = screenModelSelect
		m.modelSelect = NewModelSelect(msg.Models)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m AppModel) updateModelSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ModelSelectedMsg:
		m.screen = screenChat
		m.chat = NewChat(msg.Model.Name, msg.Model.Tag)
		// Forward the stored window size so the viewport initializes
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(tea.WindowSizeMsg{
			Width:  m.width,
			Height: m.height,
		})
		return m, cmd
	default:
		var cmd tea.Cmd
		m.modelSelect, cmd = m.modelSelect.Update(msg)
		return m, cmd
	}
}

func (m AppModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(msg)
	return m, cmd
}

func (m AppModel) View() string {
	switch m.screen {
	case screenLoading:
		if m.err != nil {
			return ErrorStyle.Render("Error: "+m.err.Error()) + "\n\n" +
				HelpStyle.Render("ctrl+c to quit")
		}
		return m.spinner.View() + " Loading models..."

	case screenModelSelect:
		return m.modelSelect.View()

	case screenChat:
		return m.chat.View()
	}

	return ""
}

// loadModels is a Cmd that fetches the docker model list.
func loadModels() tea.Msg {
	models, err := docker.ListModels()
	return ModelsLoadedMsg{Models: models, Err: err}
}
