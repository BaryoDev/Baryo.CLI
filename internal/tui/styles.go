// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Title and header styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99")).
			MarginBottom(1)

	// Model selection styles
	SelectedModelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("170")).
				Bold(true)

	NormalModelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	ModelDetailStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				MarginLeft(2)

	// Chat styles
	UserLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("82"))

	AssistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205"))

	StreamingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("141"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	HelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	SectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("69")).
				MarginBottom(0)
)
