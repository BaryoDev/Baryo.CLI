// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/arnelirobles/baryo-cli/internal/session"
)

// SessionSelectModel is the session picker screen.
type SessionSelectModel struct {
	sessions []session.Summary
	cursor   int
	width    int
	height   int
}

// NewSessionSelect creates a new session selection screen.
func NewSessionSelect(sessions []session.Summary) SessionSelectModel {
	return SessionSelectModel{
		sessions: sessions,
	}
}

func (m SessionSelectModel) Update(msg tea.Msg) (SessionSelectModel, tea.Cmd) {
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
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.sessions) > 0 {
				picked := m.sessions[m.cursor]
				return m, func() tea.Msg {
					s, err := session.Load(picked.ID)
					if err != nil {
						return SessionLoadedMsg{Err: err}
					}
					return SessionLoadedMsg{Session: s}
				}
			}
		case "esc":
			return m, func() tea.Msg {
				return SessionCancelledMsg{}
			}
		}
	}

	return m, nil
}

func (m SessionSelectModel) View() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("🐳 Baryo — Resume a Session"))
	b.WriteString("\n\n")

	if len(m.sessions) == 0 {
		b.WriteString(HelpStyle.Render("  No saved sessions found."))
		b.WriteString("\n\n")
		b.WriteString(HelpStyle.Render("esc back • ctrl+c quit"))
		return b.String()
	}

	for i, s := range m.sessions {
		cursor := "  "
		style := NormalModelStyle
		if i == m.cursor {
			cursor = "▸ "
			style = SelectedModelStyle
		}

		label := style.Render(fmt.Sprintf("%s — %s", s.ModelName, s.ID[:8]))
		detail := ModelDetailStyle.Render(
			fmt.Sprintf("%d messages • %s • %s",
				s.Messages,
				s.UpdatedAt.Format("Jan 02 15:04"),
				s.CWD))

		b.WriteString(cursor + label + "\n")
		b.WriteString("  " + detail + "\n\n")
	}

	b.WriteString(HelpStyle.Render("↑/↓ navigate • enter select • esc back • ctrl+c quit"))

	return b.String()
}
