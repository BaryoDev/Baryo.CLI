// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// adaptive returns an AdaptiveColor that picks the right shade for
// light vs dark terminal backgrounds.
func adaptive(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	// Title and header styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(adaptive("33", "75")).
			MarginBottom(1)

	// Model selection styles
	SelectedModelStyle = lipgloss.NewStyle().
				Foreground(adaptive("33", "75")).
				Bold(true)

	NormalModelStyle = lipgloss.NewStyle().
				Foreground(adaptive("236", "252"))

	ModelDetailStyle = lipgloss.NewStyle().
				Foreground(adaptive("242", "243")).
				MarginLeft(2)

	// Chat styles
	UserLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(adaptive("33", "75"))

	AssistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(adaptive("55", "183"))

	StreamingStyle = lipgloss.NewStyle().
			Foreground(adaptive("55", "183"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(adaptive("160", "167")).
			Bold(true)

	HelpStyle = lipgloss.NewStyle().
			Foreground(adaptive("242", "243"))

	SectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(adaptive("33", "75")).
				MarginBottom(0)

	ToolLabelStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(adaptive("242", "245"))

	// Tool block styles
	ToolNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(adaptive("33", "75"))

	ToolBorderStyle = lipgloss.NewStyle().
			Foreground(adaptive("250", "239"))

	ToolResultStyle = lipgloss.NewStyle().
			Foreground(adaptive("242", "245"))

	SuccessStyle = lipgloss.NewStyle().
			Foreground(adaptive("28", "108"))

	DimStyle = lipgloss.NewStyle().
			Foreground(adaptive("250", "239"))

	// Token display styles (context window usage)
	TokenDimStyle = lipgloss.NewStyle().
			Foreground(adaptive("242", "243"))

	TokenWarnStyle = lipgloss.NewStyle().
			Foreground(adaptive("172", "179"))

	TokenCritStyle = lipgloss.NewStyle().
			Foreground(adaptive("160", "167")).
			Bold(true)

	// Mention completion style
	MentionSelectedStyle = lipgloss.NewStyle().
				Foreground(adaptive("33", "75")).
				Bold(true)

	// Tab bar styles
	ActiveTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(adaptive("33", "75")).
			Background(adaptive("254", "236")).
			Padding(0, 2)

	InactiveTabStyle = lipgloss.NewStyle().
				Foreground(adaptive("242", "243")).
				Padding(0, 1)

	// Model size tag style (used in model browser)
	SizeTagStyle = lipgloss.NewStyle().
			Foreground(adaptive("242", "243"))

	// Fit tag styles (model hardware fit)
	FitFastStyle = lipgloss.NewStyle().
			Foreground(adaptive("28", "108"))

	FitSmoothStyle = lipgloss.NewStyle().
			Foreground(adaptive("33", "75"))

	FitSlowStyle = lipgloss.NewStyle().
			Foreground(adaptive("172", "179"))

	FitTooLargeStyle = lipgloss.NewStyle().
				Foreground(adaptive("160", "167"))

	// Shell mode indicator
	ShellModeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(adaptive("208", "214"))
)

// FitTagStyle returns the lipgloss style for a given fit tag.
func FitTagStyle(tag llm.FitTag) lipgloss.Style {
	switch tag {
	case llm.FitFast:
		return FitFastStyle
	case llm.FitSmooth:
		return FitSmoothStyle
	case llm.FitSlow:
		return FitSlowStyle
	case llm.FitTooLarge:
		return FitTooLargeStyle
	default:
		return HelpStyle
	}
}

// ModeStyle returns a distinct lipgloss style for each agent mode.
func ModeStyle(mode AgentMode) lipgloss.Style {
	switch mode {
	case ModeAsk:
		return lipgloss.NewStyle().Foreground(adaptive("30", "43"))
	case ModeCode:
		return lipgloss.NewStyle().Foreground(adaptive("172", "214"))
	case ModeArchitect:
		return lipgloss.NewStyle().Foreground(adaptive("55", "141"))
	case ModeReview:
		return lipgloss.NewStyle().Foreground(adaptive("166", "208"))
	case ModeResearch:
		return lipgloss.NewStyle().Foreground(adaptive("34", "78"))
	default:
		return HelpStyle
	}
}
