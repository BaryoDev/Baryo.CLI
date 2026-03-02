// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Title and header styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("75")).
			MarginBottom(1)

	// Model selection styles
	SelectedModelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("75")).
				Bold(true)

	NormalModelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	ModelDetailStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				MarginLeft(2)

	// Chat styles
	UserLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("75"))

	AssistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("183"))

	StreamingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("183"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("167")).
			Bold(true)

	HelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	SectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("75")).
				MarginBottom(0)

	ToolLabelStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("245"))

	// Tool block styles
	ToolNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("75"))

	ToolBorderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("239"))

	ToolResultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	SuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("108"))

	DimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("239"))

	// Token display styles (context window usage)
	TokenDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))

	TokenWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("179"))

	TokenCritStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("167")).
			Bold(true)

	// Mention completion style
	MentionSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("75")).
				Bold(true)

	// Tab bar styles
	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("75")).
			Background(lipgloss.Color("236")).
			Padding(0, 2)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Padding(0, 1)

	// Model size tag style (used in model browser)
	SizeTagStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))
)

// ModeStyle returns a distinct lipgloss style for each agent mode.
func ModeStyle(mode AgentMode) lipgloss.Style {
	switch mode {
	case ModeAsk:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("43"))
	case ModeCode:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case ModeArchitect:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	case ModeReview:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	case ModeResearch:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	default:
		return HelpStyle
	}
}
