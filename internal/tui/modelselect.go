// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/arnelirobles/baryo-cli/internal/docker"
)

// ModelSelectModel is the model picker screen.
type ModelSelectModel struct {
	models []docker.DockerModel
	cursor int
	width  int
	height int
}

// NewModelSelect creates a new model selection screen.
func NewModelSelect(models []docker.DockerModel) ModelSelectModel {
	return ModelSelectModel{
		models: models,
	}
}

func (m ModelSelectModel) Init() tea.Cmd {
	return nil
}

func (m ModelSelectModel) Update(msg tea.Msg) (ModelSelectModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.models)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.models) > 0 {
				return m, func() tea.Msg {
					return ModelSelectedMsg{Model: m.models[m.cursor]}
				}
			}
		}
	}

	return m, nil
}

func (m ModelSelectModel) View() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("🐳 Baryo — Select a Model"))
	b.WriteString("\n\n")

	for i, model := range m.models {
		cursor := "  "
		style := NormalModelStyle
		if i == m.cursor {
			cursor = "▸ "
			style = SelectedModelStyle
		}

		name := style.Render(model.Name)
		detail := ModelDetailStyle.Render(
			fmt.Sprintf("params: %s  size: %s", model.Params, model.Size))

		b.WriteString(cursor + name + "\n")
		b.WriteString("  " + detail + "\n\n")
	}

	b.WriteString(HelpStyle.Render("↑/↓ navigate • enter select • ctrl+c quit"))

	return b.String()
}
