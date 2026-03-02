// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import "strings"

// Intent represents the classified purpose of a user message.
type Intent int

const (
	IntentChat      Intent = iota // casual, greetings, short responses
	IntentKnowledge               // factual questions needing docs/search
	IntentPlanning                // decisions, comparisons, trade-offs
	IntentCode                    // wants code written/modified/explained
)

// ClassifyIntent determines the intent of a user message.
// Priority order: Code > Planning > Knowledge > Chat.
// Intent only adds context, never removes capabilities — wrong classification
// is harmless (e.g. a wrongly-classified Planning question just gets
// structured thinking guidance).
func ClassifyIntent(text string) Intent {
	if len(text) < 5 {
		return IntentChat
	}
	lower := strings.ToLower(text)

	// Code intent: action verbs for writing/modifying code, or code references
	// when NOT combined with comparison words.
	if isCodeIntent(lower) && !hasComparisonWords(lower) {
		return IntentCode
	}

	// Planning intent: decisions, comparisons, trade-offs.
	// Minimum 20 chars to avoid false positives on short text.
	if len(text) > 20 && isPlanningIntent(lower) {
		return IntentPlanning
	}

	// Knowledge intent: factual questions.
	if isKnowledgeIntent(lower, text) {
		return IntentKnowledge
	}

	return IntentChat
}

func isCodeIntent(lower string) bool {
	actionPrefixes := []string{
		"write a ", "write me ", "implement ", "create a ",
		"refactor ", "fix the ", "fix this ", "fix my ",
		"add a function", "add a method", "add a class",
		"build a ", "code a ", "make a function",
		"generate a ", "scaffold ",
	}
	for _, p := range actionPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	// Code action verbs anywhere
	actionVerbs := []string{
		"write a function", "write a script", "write a program",
		"implement a ", "refactor the ", "debug the ",
		"add a handler", "add an endpoint", "add a route",
	}
	for _, v := range actionVerbs {
		if strings.Contains(lower, v) {
			return true
		}
	}

	// Code file references
	codeRefs := []string{
		".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp",
		"func ", "function ", "class ", "def ", "struct ",
	}
	for _, r := range codeRefs {
		if strings.Contains(lower, r) {
			return true
		}
	}
	return false
}

func hasComparisonWords(lower string) bool {
	comparisons := []string{
		" vs ", " versus ", "compare ", "which is better",
		"pros and cons", "should i use", "trade-off", "tradeoff",
	}
	for _, c := range comparisons {
		if strings.Contains(lower, c) {
			return true
		}
	}
	return false
}

func isPlanningIntent(lower string) bool {
	phrases := []string{
		// Decision
		"should i", "which is better", "what should i", "best option",
		"what's the best", "whats the best", "which one should",
		"help me decide", "help me choose",
		// Trade-off
		"pros and cons", "compare", "versus", "vs ", "trade-off", "tradeoff",
		// Strategy / planning
		"strategize", "strategy for", "best approach", "best way to",
		"roadmap", "migration plan",
		// Purchase / life decisions
		"planning to buy", "want to buy", "looking to buy",
		"best buy", "worth buying", "worth it to",
		"recommend a ", "recommend me", "suggestion for",
		"which car", "which phone", "which laptop", "which house",
		// Architecture / tech decisions
		"should i use", "which framework", "which library", "which database",
		"which language", "monolith or microservice", "rest or graphql",
		"rest vs graphql", "sql or nosql", "sql vs nosql",
		"how should i architect", "how should i structure", "how should i design",
		"what architecture", "what tech stack", "what stack should",
	}
	for _, p := range phrases {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// General "which [noun] is/should/to" pattern — catches decision questions
	// about any topic (which car, which phone, which plan, etc.)
	if idx := strings.Index(lower, "which "); idx >= 0 {
		after := lower[idx+6:]
		decisionVerbs := []string{" is ", " should", " to buy", " to get",
			" would", " do you recommend", " is best", " are best"}
		for _, v := range decisionVerbs {
			if strings.Contains(after, v) {
				return true
			}
		}
	}

	return false
}

func isKnowledgeIntent(lower, text string) bool {
	questionPrefixes := []string{
		"what is ", "what are ", "who is ", "who are ",
		"how does ", "how do ", "how is ",
		"explain ", "tell me about ", "describe ",
		"when did ", "when was ", "where is ", "where are ",
		"why does ", "why do ", "why is ",
		"define ", "what does ", "what do ",
	}
	for _, p := range questionPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	// Ends with ? and is longer than a trivial question
	if strings.HasSuffix(strings.TrimSpace(text), "?") && len(text) > 15 {
		return true
	}
	return false
}
