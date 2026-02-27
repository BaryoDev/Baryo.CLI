// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"strings"
)

// PageSize calculates how many items fit in a list view.
// overhead is the number of lines used by non-list content (title, tabs, help, etc).
// itemHeight is the number of lines per item. minimum is the floor value.
func PageSize(height, overhead, itemHeight, minimum int) int {
	usable := height - overhead
	n := usable / itemHeight
	if n < minimum {
		return minimum
	}
	return n
}

// AdjustScroll ensures the cursor is visible within the given page size,
// adjusting the offset as needed. Returns the new offset.
func AdjustScroll(cursor, offset, pageSize int) int {
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+pageSize {
		offset = cursor - pageSize + 1
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// RenderTabBar renders a horizontal tab bar with the active tab highlighted.
func RenderTabBar(labels []string, active int) string {
	var parts []string
	for i, label := range labels {
		if i == active {
			parts = append(parts, ActiveTabStyle.Render(label))
		} else {
			parts = append(parts, InactiveTabStyle.Render(label))
		}
	}
	return "  " + strings.Join(parts, DimStyle.Render(" │ "))
}
