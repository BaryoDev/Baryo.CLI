// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"github.com/charmbracelet/glamour"
)

// RenderMarkdown renders a markdown string for terminal display.
// Falls back to the raw string if glamour fails.
func RenderMarkdown(s string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
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
