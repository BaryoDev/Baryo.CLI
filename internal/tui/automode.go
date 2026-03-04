// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"strings"
)

// ModelTier classifies a model's capability level.
type ModelTier int

const (
	TierFast   ModelTier = iota // simple chat, greetings, short answers
	TierNormal                  // standard coding, explanations
	TierStrong                  // complex reasoning, planning, multi-tool
)

// AutoModeConfig holds the pre-selected model pool for auto-routing.
type AutoModeConfig struct {
	Enabled bool
	Models  []AutoModeModel // ordered by tier (fast → strong)
}

// AutoModeModel maps a model tag to a capability tier.
type AutoModeModel struct {
	Tag  string    // model tag (e.g. "gemini/gemini-2.0-flash")
	Tier ModelTier // assigned tier
}

// SelectModel picks the optimal model for a given query based on complexity signals.
func SelectModel(cfg AutoModeConfig, query string, conversationLen int, hasToolNeeds bool) AutoModeModel {
	tier := classifyTier(query, conversationLen, hasToolNeeds)

	// Find a model at the desired tier
	for _, m := range cfg.Models {
		if m.Tier == tier {
			return m
		}
	}

	// Fallback: find the closest tier above, then below
	for t := tier + 1; t <= TierStrong; t++ {
		for _, m := range cfg.Models {
			if m.Tier == t {
				return m
			}
		}
	}
	for t := tier - 1; t >= TierFast; t-- {
		for _, m := range cfg.Models {
			if m.Tier == t {
				return m
			}
		}
	}

	// Should never happen if config is valid, but return first model as ultimate fallback
	if len(cfg.Models) > 0 {
		return cfg.Models[0]
	}
	return AutoModeModel{}
}

// classifyTier determines the minimum model tier needed for a query.
func classifyTier(query string, conversationLen int, hasToolNeeds bool) ModelTier {
	// Long conversations need strong models for context management
	if conversationLen > 20 {
		return TierStrong
	}

	intent := ClassifyIntent(query)
	lower := strings.ToLower(query)

	switch intent {
	case IntentPlanning:
		return TierStrong

	case IntentCode:
		if isComplexCodeQuery(lower) {
			return TierStrong
		}
		return TierNormal

	case IntentKnowledge:
		return TierNormal

	case IntentChat:
		if len(query) < 50 {
			return TierFast
		}
		return TierNormal
	}

	// Tool needs bump to at least Normal
	if hasToolNeeds {
		return TierNormal
	}

	return TierNormal
}

// isComplexCodeQuery detects queries that need strong reasoning.
func isComplexCodeQuery(lower string) bool {
	complexKeywords := []string{
		"refactor", "implement", "architect", "redesign", "migrate",
		"optimize", "rewrite", "overhaul", "restructure",
	}
	for _, kw := range complexKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// TierName returns a display string for a tier.
func TierName(t ModelTier) string {
	switch t {
	case TierFast:
		return "fast"
	case TierNormal:
		return "normal"
	case TierStrong:
		return "strong"
	}
	return "unknown"
}

// ParseTier converts a tier string from config to ModelTier.
func ParseTier(s string) ModelTier {
	switch strings.ToLower(s) {
	case "fast":
		return TierFast
	case "strong":
		return TierStrong
	default:
		return TierNormal
	}
}
