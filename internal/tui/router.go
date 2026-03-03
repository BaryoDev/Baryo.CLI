// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"github.com/arnelirobles/baryo-cli/internal/rag"
)

// RouteAction indicates what the router decided to do with the user's input.
type RouteAction int

const (
	RouteDefault  RouteAction = iota // normal stream
	RouteSearch                      // trigger /search pipeline
	RouteResearch                    // trigger /research pipeline
	RouteRemember                    // auto-remember
)

// routeResult holds the router's decision and any extracted query.
type routeResult struct {
	Action RouteAction
	Query  string
}

// routeInput consolidates all heuristic pre-stream routing for small models
// (contextLimit < 32K) into a single function with clear priority ordering.
// It reads state but produces no side effects — the caller acts on the result.
//
// Priority order:
//  1. Conversational shortcuts (search agreement, remember agreement)
//  2. Natural language research intent (skip if IntentPlanning)
//  3. Proactive search (NeedsFreshInfo + IntentKnowledge + empty RAG)
//  4. RouteDefault
func (m *ChatModel) routeInput(text string) routeResult {
	// 1. Search agreement: user said "yes" after model suggested searching.
	if m.isSearchAgreement(text) {
		if query := m.extractSearchTopic(); query != "" {
			return routeResult{Action: RouteSearch, Query: query}
		}
	}

	// 2. Remember agreement: user said "yes" after model suggested /remember.
	if m.isRememberAgreement(text) {
		if fact := m.extractRememberFact(); fact != "" {
			return routeResult{Action: RouteRemember, Query: fact}
		}
	}

	// 3. Natural language research intent: "research X", "deep dive into X", etc.
	//    Skip if intent is Planning — those get structured thinking instead.
	if ClassifyIntent(text) != IntentPlanning {
		if topic := isResearchIntent(text); topic != "" {
			return routeResult{Action: RouteResearch, Query: topic}
		}
	}

	// 4. Proactive web search: trigger when the query needs fresh info and
	//    RAG has nothing relevant.
	if !m.searchPending && !m.searchFallbackUsed {
		needsSearch := false
		if m.ragPipeline != nil && m.ragPipeline.NeedsFreshInfo(text) {
			needsSearch = true
		} else if ClassifyIntent(text) == IntentKnowledge && m.ragPrompt == "" && rag.NeedsFreshInfo(text) {
			needsSearch = true
		}
		if needsSearch {
			return routeResult{Action: RouteSearch, Query: text}
		}
	}

	return routeResult{Action: RouteDefault}
}
