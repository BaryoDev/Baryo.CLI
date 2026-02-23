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
	offset   int // first visible item index
	width    int
	height   int
}

// NewSessionSelect creates a new session selection screen.
func NewSessionSelect(sessions []session.Summary) SessionSelectModel {
	return SessionSelectModel{
		sessions: sessions,
	}
}

// pageSize returns how many items fit on screen (minimum 5).
func (m *SessionSelectModel) pageSize() int {
	// title(2) + help bar(1) + scroll indicators(2) = ~5 overhead
	// each session takes ~3 lines (label + detail + blank)
	usable := m.height - 5
	n := usable / 3
	if n < 5 {
		return 5
	}
	return n
}

// adjustScroll ensures the cursor is within the visible window.
func (m *SessionSelectModel) adjustScroll() {
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

func (m SessionSelectModel) Update(msg tea.Msg) (SessionSelectModel, tea.Cmd) {
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
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
				m.adjustScroll()
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

	// Determine visible window
	ps := m.pageSize()
	end := m.offset + ps
	if end > len(m.sessions) {
		end = len(m.sessions)
	}

	// Scroll indicator at top
	if m.offset > 0 {
		b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↑ %d more above", m.offset)))
		b.WriteString("\n\n")
	}

	// Render only visible items
	for i := m.offset; i < end; i++ {
		s := m.sessions[i]

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

	// Scroll indicator at bottom
	remaining := len(m.sessions) - end
	if remaining > 0 {
		b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n\n")
	}

	b.WriteString(HelpStyle.Render("↑/↓ navigate • enter select • esc back • ctrl+c quit"))

	return b.String()
}
