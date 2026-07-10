// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
)

func boolPtr(b bool) *bool       { return &b }
func uintPtr(u uint) *uint       { return &u }
func stringPtr(s string) *string { return &s }

// baryoStyle is a custom glamour style based on "dark" but with brighter
// body text (color 255 / white) for better readability.
var baryoStyle = ansi.StyleConfig{
	Document: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "\n",
			BlockSuffix: "\n",
			Color:       stringPtr("255"),
		},
		Margin: uintPtr(2),
	},
	BlockQuote: ansi.StyleBlock{
		Indent:      uintPtr(1),
		IndentToken: stringPtr("│ "),
	},
	List: ansi.StyleList{
		LevelIndent: 2,
	},
	Heading: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       stringPtr("39"),
			Bold:        boolPtr(true),
		},
	},
	H1: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix:          " ",
			Suffix:          " ",
			Color:           stringPtr("228"),
			BackgroundColor: stringPtr("63"),
			Bold:            boolPtr(true),
		},
	},
	H2: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "## ",
			Color:  stringPtr("39"),
			Bold:   boolPtr(true),
		},
	},
	H3: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "### ",
			Color:  stringPtr("75"),
			Bold:   boolPtr(true),
		},
	},
	Text:          ansi.StylePrimitive{},
	Strikethrough: ansi.StylePrimitive{CrossedOut: boolPtr(true)},
	Emph:          ansi.StylePrimitive{Italic: boolPtr(true)},
	Strong:        ansi.StylePrimitive{Bold: boolPtr(true)},
	HorizontalRule: ansi.StylePrimitive{
		Color:  stringPtr("240"),
		Format: "\n--------\n",
	},
	Item: ansi.StylePrimitive{
		BlockPrefix: "• ",
	},
	Enumeration: ansi.StylePrimitive{
		BlockPrefix: ". ",
	},
	Task: ansi.StyleTask{
		Ticked:   "[✓] ",
		Unticked: "[ ] ",
	},
	Link: ansi.StylePrimitive{
		Color:     stringPtr("30"),
		Underline: boolPtr(true),
	},
	LinkText: ansi.StylePrimitive{
		Color: stringPtr("35"),
		Bold:  boolPtr(true),
	},
	Image: ansi.StylePrimitive{
		Color:     stringPtr("212"),
		Underline: boolPtr(true),
	},
	ImageText: ansi.StylePrimitive{
		Color:  stringPtr("243"),
		Format: "Image: {{.text}} →",
	},
	Code: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix:          " ",
			Suffix:          " ",
			Color:           stringPtr("203"),
			BackgroundColor: stringPtr("236"),
		},
	},
	CodeBlock: ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("244"),
			},
			Margin: uintPtr(2),
		},
		Chroma: &ansi.Chroma{
			Text: ansi.StylePrimitive{
				Color: stringPtr("#C4C4C4"),
			},
			Comment: ansi.StylePrimitive{
				Color: stringPtr("#6272A4"),
			},
			Keyword: ansi.StylePrimitive{
				Color: stringPtr("#00AAFF"),
			},
			KeywordType: ansi.StylePrimitive{
				Color: stringPtr("#8BE9FD"),
			},
			LiteralString: ansi.StylePrimitive{
				Color: stringPtr("#C69669"),
			},
			LiteralNumber: ansi.StylePrimitive{
				Color: stringPtr("#BD93F9"),
			},
			NameFunction: ansi.StylePrimitive{
				Color: stringPtr("#00D787"),
			},
			NameClass: ansi.StylePrimitive{
				Color: stringPtr("#8BE9FD"),
			},
			Operator: ansi.StylePrimitive{
				Color: stringPtr("#FF79C6"),
			},
		},
	},
	Table: ansi.StyleTable{},
}

// RenderMarkdown renders a markdown string for terminal display.
// Falls back to the raw string if glamour fails.
func RenderMarkdown(s string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(baryoStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return s
	}
	out, err := r.Render(s)
	if err != nil {
		return s
	}
	return out
}
