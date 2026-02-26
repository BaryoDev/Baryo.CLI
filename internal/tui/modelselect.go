// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/docker"
)

// ModelSelectModel is the model picker screen.
type ModelSelectModel struct {
	models []docker.DockerModel
	cursor int
	offset int // scroll offset (first visible item index)
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

// pageSize returns how many items fit on screen.
func (m *ModelSelectModel) pageSize() int {
	// title(2) + help bar(1) + scroll indicators(2) = ~5 overhead
	// each item takes ~3 lines (name + detail + blank)
	usable := m.height - 5
	n := usable / 3
	if n < 5 {
		return 5
	}
	return n
}

// adjustScroll ensures the cursor is within the visible window.
func (m *ModelSelectModel) adjustScroll() {
	ps := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+ps {
		m.offset = m.cursor - ps + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m ModelSelectModel) Update(msg tea.Msg) (ModelSelectModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustScroll()

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.adjustScroll()
			}
		case "down", "j":
			if m.cursor < len(m.models)-1 {
				m.cursor++
				m.adjustScroll()
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

	providerTagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	// Determine visible window
	ps := m.pageSize()
	end := m.offset + ps
	if end > len(m.models) {
		end = len(m.models)
	}

	// Scroll indicator at top
	if m.offset > 0 {
		b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↑ %d more above", m.offset)))
		b.WriteString("\n\n")
	}

	for i := m.offset; i < end; i++ {
		model := m.models[i]
		cursor := "  "
		style := NormalModelStyle
		if i == m.cursor {
			cursor = "▸ "
			style = SelectedModelStyle
		}

		name := style.Render(model.Name)
		if model.Provider != "" {
			name += " " + providerTagStyle.Render("["+model.Provider+"]")
		}

		var detail string
		if model.Provider != "" {
			detail = ModelDetailStyle.Render("cloud model")
		} else {
			detail = ModelDetailStyle.Render(
				fmt.Sprintf("params: %s  size: %s", model.Params, model.Size))
		}

		b.WriteString(cursor + name + "\n")
		b.WriteString("  " + detail + "\n\n")
	}

	// Scroll indicator at bottom
	remaining := len(m.models) - end
	if remaining > 0 {
		b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n\n")
	}

	b.WriteString(HelpStyle.Render("↑/↓ navigate • enter select • ctrl+c quit"))

	return b.String()
}
