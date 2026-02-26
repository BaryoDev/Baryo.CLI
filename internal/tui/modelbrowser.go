// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/docker"
)

// modelItem represents a single entry in the flat list.
type modelItem struct {
	name       string
	detail     string
	size       string // memory/disk size (e.g. "4.07 GiB"), empty if unknown
	downloaded bool
	model      docker.DockerModel
	search     docker.SearchModel
}

// ModelBrowserModel is the model browser screen.
type ModelBrowserModel struct {
	downloaded []docker.DockerModel
	available  []docker.SearchModel
	items      []modelItem
	cursor     int
	offset     int // scroll offset (first visible item index)

	pulling    bool
	pullName   string
	pullStatus string
	pullCh     <-chan string
	pullCancel context.CancelFunc
	spinner    spinner.Model

	err    error
	width  int
	height int
}

// NewModelBrowser creates a new model browser from the fetched lists.
func NewModelBrowser(downloaded []docker.DockerModel, available []docker.SearchModel) ModelBrowserModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("205"))),
	)
	m := ModelBrowserModel{
		downloaded: downloaded,
		available:  available,
		spinner:    s,
	}
	m.buildItems()
	return m
}

func (m *ModelBrowserModel) buildItems() {
	downloadedNames := make(map[string]bool, len(m.downloaded))
	for _, d := range m.downloaded {
		downloadedNames[d.Name] = true
	}

	m.items = nil

	for _, d := range m.downloaded {
		detail := fmt.Sprintf("params: %s  quantized size: %s", d.Params, d.Size)
		if d.Provider != "" {
			detail = "cloud model"
		}
		m.items = append(m.items, modelItem{
			name:       d.Name,
			detail:     detail,
			size:       d.Size,
			downloaded: true,
			model:      d,
		})
	}

	for _, s := range m.available {
		if downloadedNames[s.Name] {
			continue
		}
		m.items = append(m.items, modelItem{
			name:       s.Name,
			detail:     s.Description,
			downloaded: false,
			search:     s,
		})
	}
}

// pageSize returns how many items fit on screen (minimum 5).
func (m *ModelBrowserModel) pageSize() int {
	// title(2) + section header(2) + help bar(1) + scroll indicators(2) = ~7 overhead
	// each item takes ~3 lines (name + detail + blank)
	usable := m.height - 7
	n := usable / 3
	if n < 5 {
		return 5
	}
	return n
}

// adjustScroll ensures the cursor is within the visible window.
func (m *ModelBrowserModel) adjustScroll() {
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

func (m ModelBrowserModel) Update(msg tea.Msg) (ModelBrowserModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustScroll()

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if !m.pulling && m.cursor > 0 {
				m.cursor--
				m.adjustScroll()
			}
		case "down", "j":
			if !m.pulling && m.cursor < len(m.items)-1 {
				m.cursor++
				m.adjustScroll()
			}
		case "enter":
			if m.pulling || len(m.items) == 0 {
				break
			}
			item := m.items[m.cursor]
			if item.downloaded {
				selected := item.model
				return m, func() tea.Msg {
					return ModelSelectedMsg{Model: selected}
				}
			}
			// Start streaming pull
			ctx, cancel := context.WithCancel(context.Background())
			m.pulling = true
			m.pullName = item.name
			m.pullStatus = "Starting pull..."
			m.pullCancel = cancel
			m.pullCh = docker.StreamPull(ctx, item.name)
			return m, tea.Batch(m.spinner.Tick, waitForPullLine(m.pullCh))
		case "esc":
			if m.pulling && m.pullCancel != nil {
				m.pullCancel()
			}
			if !m.pulling {
				return m, func() tea.Msg {
					return ModelBrowserCancelMsg{}
				}
			}
		}

	case PullStatusMsg:
		if msg.Done {
			m.pulling = false
			pullHadError := strings.HasPrefix(m.pullStatus, "error:")
			m.pullStatus = ""
			m.pullCh = nil
			if m.pullCancel != nil {
				m.pullCancel()
				m.pullCancel = nil
			}
			if pullHadError {
				m.err = fmt.Errorf("pull failed for %s", m.pullName)
				return m, nil
			}
			// Refresh lists after successful pull
			avail := m.available
			return m, func() tea.Msg {
				downloaded, err := docker.ListModels()
				if err != nil {
					return ShowModelsMsg{Err: err}
				}
				return ShowModelsMsg{Downloaded: downloaded, Available: avail}
			}
		}
		m.pullStatus = msg.Status
		return m, waitForPullLine(m.pullCh)

	case spinner.TickMsg:
		if m.pulling {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m ModelBrowserModel) View() string {
	var b strings.Builder

	installedTag := lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("[installed]")
	availableTag := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("[available]")
	sizeTag := lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	providerTagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	b.WriteString(TitleStyle.Render("🐳 Baryo — Model Browser"))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(ErrorStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n\n")
	}

	if m.pulling {
		b.WriteString(fmt.Sprintf("  %s Pulling %s\n", m.spinner.View(), m.pullName))
		if m.pullStatus != "" {
			b.WriteString("  " + HelpStyle.Render(m.pullStatus))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(HelpStyle.Render("esc cancel • ctrl+c quit"))
		return b.String()
	}

	if len(m.items) == 0 {
		b.WriteString(HelpStyle.Render("  No models found."))
		b.WriteString("\n\n")
		b.WriteString(HelpStyle.Render("esc back • ctrl+c quit"))
		return b.String()
	}

	// Determine visible window
	ps := m.pageSize()
	end := m.offset + ps
	if end > len(m.items) {
		end = len(m.items)
	}

	// Show scroll indicator at top
	if m.offset > 0 {
		b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↑ %d more above", m.offset)))
		b.WriteString("\n\n")
	}

	// Render section headers + items in the visible window
	shownDownloadedHeader := false
	shownAvailableHeader := false

	for i := m.offset; i < end; i++ {
		item := m.items[i]

		if item.downloaded && !shownDownloadedHeader {
			// Only show header if there are downloaded items in or before the window
			shownDownloadedHeader = true
			b.WriteString(SectionHeaderStyle.Render("── Downloaded ──"))
			b.WriteString("\n\n")
		}
		if !item.downloaded && !shownAvailableHeader {
			shownAvailableHeader = true
			b.WriteString(SectionHeaderStyle.Render("── Available (Docker Hub) ──"))
			b.WriteString("\n\n")
		}

		cursor := "  "
		style := NormalModelStyle
		if i == m.cursor {
			cursor = "▸ "
			style = SelectedModelStyle
		}

		tag := availableTag
		if item.downloaded {
			tag = installedTag
		}
		if item.model.Provider != "" {
			tag = providerTagStyle.Render("[" + item.model.Provider + "]")
		}

		sizeInfo := ""
		if item.size != "" {
			sizeInfo = " " + sizeTag.Render(item.size)
		}

		b.WriteString(fmt.Sprintf("%s%s %s%s\n", cursor, style.Render(item.name), tag, sizeInfo))
		b.WriteString("    " + ModelDetailStyle.Render(item.detail) + "\n\n")
	}

	// Show scroll indicator at bottom
	remaining := len(m.items) - end
	if remaining > 0 {
		b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n\n")
	}

	b.WriteString(HelpStyle.Render("↑/↓ navigate • enter select/pull • esc back • ctrl+c quit"))

	return b.String()
}

// waitForPullLine returns a Cmd that waits for the next line from the pull channel.
func waitForPullLine(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return PullStatusMsg{Done: true}
		}
		return PullStatusMsg{Status: line}
	}
}
