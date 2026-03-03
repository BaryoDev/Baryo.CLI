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
	"current version",
	"current price",
	"current status",
	"currently available",
	"recent",
	"news about",
	"what happened",
	"right now",
	"this week",
	"this month",
	"this year",
	"breaking",
	"update on",
	"just released",
	"new release",
	"announced",
}

// looksLikeQuestion returns true if the query looks like a question rather than
// a code discussion or statement. Used to guard year-based freshness detection
// against false positives from incidental year mentions.
func looksLikeQuestion(lower string) bool {
	if strings.HasSuffix(strings.TrimSpace(lower), "?") {
		return true
	}
	questionWords := []string{"what ", "when ", "where ", "which ", "who ", "how ", "is ", "are ", "does ", "do ", "can ", "should "}
	for _, w := range questionWords {
		if strings.HasPrefix(lower, w) {
			return true
		}
	}
	intentWords := []string{"best", "recommend", "comparison", "compare", "versus", " vs "}
	for _, w := range intentWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// NeedsFreshInfo returns true if the query appears to need up-to-date web information.
func NeedsFreshInfo(query string) bool {
	lower := strings.ToLower(query)

	// Check for current/near-future year references, but only when the query
	// looks like a question — prevents code discussion years from triggering.
	year := time.Now().Year()
	for y := year - 1; y <= year+1; y++ {
		if strings.Contains(lower, strconv.Itoa(y)) && looksLikeQuestion(lower) {
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
