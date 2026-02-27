// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// modelItem represents a single entry in a browser tab.
type modelItem struct {
	name       string
	detail     string
	size       string // memory/disk size (e.g. "4.07 GiB"), empty if unknown
	downloaded bool
	model      llm.Model
	search     llm.SearchModel
}

// browserTab groups items under a named tab.
type browserTab struct {
	label string
	items []modelItem
}

// ModelBrowserModel is the tabbed model browser screen.
type ModelBrowserModel struct {
	downloaded []llm.Model
	available  []llm.SearchModel
	tabs      []browserTab
	activeTab int
	cursor    int
	offset    int // scroll offset within the active tab

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

// NewModelBrowser creates a new tabbed model browser from the fetched lists.
func NewModelBrowser(downloaded []llm.Model, available []llm.SearchModel) ModelBrowserModel {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("183"))),
	)
	m := ModelBrowserModel{
		downloaded: downloaded,
		available:  available,
		spinner:    s,
	}
	m.buildTabs()
	return m
}

func (m *ModelBrowserModel) buildTabs() {
	downloadedNames := make(map[string]bool, len(m.downloaded))
	for _, d := range m.downloaded {
		downloadedNames[d.Name] = true
	}

	// Group downloaded models: local vs providers.
	var localItems []modelItem
	providerItems := make(map[string][]modelItem)

	for _, d := range m.downloaded {
		item := modelItem{
			name:       d.Name,
			downloaded: true,
			model:      d,
		}
		if d.Provider != "" {
			if d.PromptPrice > 0 {
				item.detail = formatPricing(d.PromptPrice, d.CompletionPrice)
			} else {
				item.detail = "cloud model"
			}
			if d.Params != "" {
				item.detail = d.Params + "  " + item.detail
			}
			providerItems[d.Provider] = append(providerItems[d.Provider], item)
		} else {
			item.detail = fmt.Sprintf("params: %s  size: %s", d.Params, d.Size)
			item.size = d.Size
			localItems = append(localItems, item)
		}
	}

	// Available Docker Hub models.
	var hubItems []modelItem
	for _, s := range m.available {
		if downloadedNames[s.Name] {
			continue
		}
		hubItems = append(hubItems, modelItem{
			name:       s.Name,
			detail:     s.Description,
			downloaded: false,
			search:     s,
		})
	}

	m.tabs = nil

	if len(localItems) > 0 {
		m.tabs = append(m.tabs, browserTab{label: "Local", items: localItems})
	}

	// Sort provider names.
	providerNames := make([]string, 0, len(providerItems))
	for p := range providerItems {
		providerNames = append(providerNames, p)
	}
	sort.Strings(providerNames)

	for _, p := range providerNames {
		label := strings.ToUpper(p[:1]) + p[1:]
		m.tabs = append(m.tabs, browserTab{label: label, items: providerItems[p]})
	}

	if len(hubItems) > 0 {
		m.tabs = append(m.tabs, browserTab{label: "Docker Hub", items: hubItems})
	}

	if len(m.tabs) == 0 {
		m.tabs = append(m.tabs, browserTab{label: "Local"})
	}
}

// activeItems returns the items in the currently active tab.
func (m *ModelBrowserModel) activeItems() []modelItem {
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab].items
	}
	return nil
}

// pageSize returns how many items fit on screen (minimum 3).
func (m *ModelBrowserModel) pageSize() int {
	return PageSize(m.height, 7, 3, 3)
}

// adjustScroll ensures the cursor is within the visible window.
func (m *ModelBrowserModel) adjustScroll() {
	m.offset = AdjustScroll(m.cursor, m.offset, m.pageSize())
}

func (m ModelBrowserModel) Update(msg tea.Msg) (ModelBrowserModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustScroll()

	case tea.KeyMsg:
		if m.pulling {
			if msg.String() == "esc" && m.pullCancel != nil {
				m.pullCancel()
			}
			break
		}
		items := m.activeItems()
		switch msg.String() {
		case "tab", "right", "l":
			if m.activeTab < len(m.tabs)-1 {
				m.activeTab++
				m.cursor = 0
				m.offset = 0
			}
		case "shift+tab", "left", "h":
			if m.activeTab > 0 {
				m.activeTab--
				m.cursor = 0
				m.offset = 0
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.adjustScroll()
			}
		case "down", "j":
			if m.cursor < len(items)-1 {
				m.cursor++
				m.adjustScroll()
			}
		case "enter":
			if len(items) == 0 {
				break
			}
			item := items[m.cursor]
			if item.downloaded {
				selected := item.model
				return m, func() tea.Msg {
					return ModelSelectedMsg{Model: selected}
				}
			}
			// Start streaming pull.
			ctx, cancel := context.WithCancel(context.Background())
			m.pulling = true
			m.pullName = item.name
			m.pullStatus = "Starting pull..."
			m.pullCancel = cancel
			m.pullCh = llm.StreamPull(ctx, item.name)
			return m, tea.Batch(m.spinner.Tick, waitForPullLine(m.pullCh))
		case "esc":
			return m, func() tea.Msg {
				return ModelBrowserCancelMsg{}
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
			// Refresh lists after successful pull.
			avail := m.available
			return m, func() tea.Msg {
				downloaded, err := llm.ListModels()
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

	b.WriteString(TitleStyle.Render("baryo") + DimStyle.Render(" · ") + HelpStyle.Render("model browser"))
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
		b.WriteString(HelpStyle.Render("esc cancel · ctrl+c quit"))
		return b.String()
	}

	// Render tab bar.
	var tabLabels []string
	for _, tab := range m.tabs {
		tabLabels = append(tabLabels, fmt.Sprintf("%s (%d)", tab.label, len(tab.items)))
	}
	b.WriteString(RenderTabBar(tabLabels, m.activeTab))
	b.WriteString("\n\n")

	// Render items for the active tab.
	items := m.activeItems()

	if len(items) == 0 {
		b.WriteString(HelpStyle.Render("  No models in this tab."))
		b.WriteString("\n\n")
	} else {
		sizeTag := SizeTagStyle

		ps := m.pageSize()
		end := m.offset + ps
		if end > len(items) {
			end = len(items)
		}

		if m.offset > 0 {
			b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↑ %d more above", m.offset)))
			b.WriteString("\n\n")
		}

		for i := m.offset; i < end; i++ {
			item := items[i]
			cursor := "  "
			style := NormalModelStyle
			if i == m.cursor {
				cursor = "▸ "
				style = SelectedModelStyle
			}

			sizeInfo := ""
			if item.size != "" {
				sizeInfo = " " + sizeTag.Render(item.size)
			}

			b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, style.Render(item.name), sizeInfo))
			b.WriteString("    " + ModelDetailStyle.Render(item.detail) + "\n\n")
		}

		remaining := len(items) - end
		if remaining > 0 {
			b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
			b.WriteString("\n\n")
		}
	}

	b.WriteString(HelpStyle.Render("tab/←/→ switch tab · ↑/↓ navigate · enter select/pull · esc back · ctrl+c quit"))

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
