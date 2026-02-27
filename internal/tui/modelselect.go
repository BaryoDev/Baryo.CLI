// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/docker"
)

// modelTab groups models under a named tab.
type modelTab struct {
	label  string // display label (e.g. "Local", "Groq", "Bedrock")
	models []docker.DockerModel
}

// ModelSelectModel is the tabbed model picker screen.
type ModelSelectModel struct {
	tabs     []modelTab
	activeTab int // which tab is selected
	cursor    int // cursor within the active tab's model list
	offset    int // scroll offset within the active tab
	width     int
	height    int
}

// NewModelSelect creates a new tabbed model selection screen.
func NewModelSelect(models []docker.DockerModel) ModelSelectModel {
	m := ModelSelectModel{}
	m.tabs = buildTabs(models)
	return m
}

// buildTabs groups models into tabs: "Local" first, then one tab per provider.
func buildTabs(models []docker.DockerModel) []modelTab {
	var local []docker.DockerModel
	providerMap := make(map[string][]docker.DockerModel)

	for _, m := range models {
		if m.Provider == "" {
			local = append(local, m)
		} else {
			providerMap[m.Provider] = append(providerMap[m.Provider], m)
		}
	}

	var tabs []modelTab

	// Local tab always comes first (even if empty, so user sees it).
	if len(local) > 0 {
		tabs = append(tabs, modelTab{label: "Local", models: local})
	}

	// Sort provider names for stable ordering.
	providerNames := make([]string, 0, len(providerMap))
	for p := range providerMap {
		providerNames = append(providerNames, p)
	}
	sort.Strings(providerNames)

	for _, p := range providerNames {
		// Capitalize first letter for display.
		label := strings.ToUpper(p[:1]) + p[1:]
		tabs = append(tabs, modelTab{label: label, models: providerMap[p]})
	}

	// Edge case: no models at all.
	if len(tabs) == 0 {
		tabs = append(tabs, modelTab{label: "Local"})
	}

	return tabs
}

func (m ModelSelectModel) Init() tea.Cmd {
	return nil
}

// pageSize returns how many items fit on screen.
func (m *ModelSelectModel) pageSize() int {
	// title(1) + tabs(2) + help bar(1) + scroll indicators(2) + spacing = ~7 overhead
	// each item takes 2 lines (name + detail) + 1 blank
	usable := m.height - 7
	n := usable / 3
	if n < 3 {
		return 3
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

// activeModels returns the models in the currently active tab.
func (m *ModelSelectModel) activeModels() []docker.DockerModel {
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab].models
	}
	return nil
}

func (m ModelSelectModel) Update(msg tea.Msg) (ModelSelectModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustScroll()

	case tea.KeyMsg:
		models := m.activeModels()
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
			if m.cursor < len(models)-1 {
				m.cursor++
				m.adjustScroll()
			}
		case "enter":
			if len(models) > 0 {
				selected := models[m.cursor]
				return m, func() tea.Msg {
					return ModelSelectedMsg{Model: selected}
				}
			}
		}
	}

	return m, nil
}

func (m ModelSelectModel) View() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("baryo") + DimStyle.Render(" · ") + HelpStyle.Render("select a model"))
	b.WriteString("\n\n")

	// Render tab bar.
	activeTabStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("75")).
		Background(lipgloss.Color("236")).
		Padding(0, 2)

	inactiveTabStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Padding(0, 1)

	var tabParts []string
	for i, tab := range m.tabs {
		label := tab.label
		count := len(tab.models)
		text := fmt.Sprintf("%s (%d)", label, count)
		if i == m.activeTab {
			tabParts = append(tabParts, activeTabStyle.Render(text))
		} else {
			tabParts = append(tabParts, inactiveTabStyle.Render(text))
		}
	}
	b.WriteString("  " + strings.Join(tabParts, DimStyle.Render(" │ ")))
	b.WriteString("\n\n")

	// Render models for the active tab.
	models := m.activeModels()

	if len(models) == 0 {
		b.WriteString(HelpStyle.Render("  No models available in this tab."))
		b.WriteString("\n\n")
	} else {
		ps := m.pageSize()
		end := m.offset + ps
		if end > len(models) {
			end = len(models)
		}

		// Scroll indicator at top.
		if m.offset > 0 {
			b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↑ %d more above", m.offset)))
			b.WriteString("\n\n")
		}

		for i := m.offset; i < end; i++ {
			model := models[i]
			cursor := "  "
			style := NormalModelStyle
			if i == m.cursor {
				cursor = "▸ "
				style = SelectedModelStyle
			}

			name := style.Render(model.Name)

			var detail string
			if model.Provider != "" {
				if model.PromptPrice > 0 {
					detail = ModelDetailStyle.Render(formatPricing(model.PromptPrice, model.CompletionPrice))
				} else {
					detail = ModelDetailStyle.Render("cloud model")
				}
				if model.Params != "" {
					detail = ModelDetailStyle.Render(model.Params) + "  " + detail
				}
			} else {
				detail = ModelDetailStyle.Render(
					fmt.Sprintf("params: %s  size: %s", model.Params, model.Size))
			}

			b.WriteString(cursor + name + "\n")
			b.WriteString("  " + detail + "\n\n")
		}

		// Scroll indicator at bottom.
		remaining := len(models) - end
		if remaining > 0 {
			b.WriteString(HelpStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
			b.WriteString("\n\n")
		}
	}

	b.WriteString(HelpStyle.Render("tab/←/→ switch tab · ↑/↓ navigate · enter select · ctrl+c quit"))

	return b.String()
}

// formatPricing formats per-token prices as a human-readable string.
func formatPricing(promptPrice, completionPrice float64) string {
	fmtPrice := func(p float64) string {
		perMillion := p * 1_000_000
		if perMillion < 0.1 {
			return fmt.Sprintf("$%.4f/M", perMillion)
		}
		if perMillion < 1.0 {
			return fmt.Sprintf("$%.2f/M", perMillion)
		}
		return fmt.Sprintf("$%.1f/M", perMillion)
	}
	return fmt.Sprintf("in: %s  out: %s", fmtPrice(promptPrice), fmtPrice(completionPrice))
}
