// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rag

import (
	"strconv"
	"strings"
	"time"
)

// freshnessKeywords are phrases that signal time-sensitive queries.
var freshnessKeywords = []string{
	"today",
	"latest",
	"current",
	"recent",
	"news about",
	"what happened",
	"right now",
	"this week",
	"this month",
	"this year",
	"breaking",
	"update on",
	"updated",
	"just released",
	"new release",
	"announced",
}

// NeedsFreshInfo returns true if the query appears to need up-to-date web information.
func NeedsFreshInfo(query string) bool {
	lower := strings.ToLower(query)

	// Check for current/near-future year references.
	year := time.Now().Year()
	for y := year - 1; y <= year+1; y++ {
		if strings.Contains(lower, strconv.Itoa(y)) {
			return true
		}
	}

	for _, kw := range freshnessKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
