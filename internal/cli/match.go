// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package cli

import (
	"fmt"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// MatchModel finds a model by query using three-pass matching:
// 1. Exact name match
// 2. Short name match (after last '/')
// 3. Case-insensitive substring match
func MatchModel(query string, models []llm.Model) (llm.Model, error) {
	if len(models) == 0 {
		return llm.Model{}, fmt.Errorf("no models available")
	}

	lower := strings.ToLower(query)

	// Pass 1: exact name match
	for _, m := range models {
		if m.Name == query {
			return m, nil
		}
	}

	// Pass 2: short name match (part after last '/')
	for _, m := range models {
		if idx := strings.LastIndex(m.Name, "/"); idx != -1 {
			if m.Name[idx+1:] == query {
				return m, nil
			}
		}
	}

	// Pass 3: case-insensitive substring
	var matches []llm.Model
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.Name), lower) {
			matches = append(matches, m)
		}
	}

	switch len(matches) {
	case 0:
		names := make([]string, len(models))
		for i, m := range models {
			names[i] = m.Name
		}
		return llm.Model{}, fmt.Errorf("no model matching %q\navailable models: %s", query, strings.Join(names, ", "))
	case 1:
		return matches[0], nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return llm.Model{}, fmt.Errorf("ambiguous model %q matches: %s", query, strings.Join(names, ", "))
	}
}
