// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package search

import (
	"context"
	"fmt"
	"strings"
)

// sendProgress safely sends a status update to the progress channel.
// It is a no-op if ch is nil and respects context cancellation.
func sendProgress(ctx context.Context, ch chan<- string, msg string) {
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	case <-ctx.Done():
	}
}

// ResearchDepth controls how many search rounds to run.
type ResearchDepth int

const (
	DepthQuick    ResearchDepth = 1
	DepthStandard ResearchDepth = 3
	DepthDeep     ResearchDepth = 5
)

// ParseDepth interprets a depth keyword. Returns the depth and the remaining
// topic string with the keyword stripped.
func ParseDepth(input string) (ResearchDepth, string) {
	parts := strings.SplitN(strings.TrimSpace(input), " ", 2)
	if len(parts) < 2 {
		return DepthStandard, strings.TrimSpace(input)
	}
	switch strings.ToLower(parts[0]) {
	case "quick":
		return DepthQuick, strings.TrimSpace(parts[1])
	case "deep":
		return DepthDeep, strings.TrimSpace(parts[1])
	default:
		return DepthStandard, strings.TrimSpace(input)
	}
}

// ResearchSource tracks a single source encountered during research.
type ResearchSource struct {
	Title string
	URL   string
	Round int
}

// ResearchResult is the output of a multi-round research pipeline.
type ResearchResult struct {
	Topic              string
	AccumulatedContext string // all round findings concatenated
	Sources            []ResearchSource
	Rounds             int
}

// ModelCallFunc is the signature for a blocking model call used during
// intermediate analysis. It sends messages to the model and returns the
// full response text.
type ModelCallFunc func(ctx context.Context, prompt string) (string, error)

// ResearchConfig configures a research run.
type ResearchConfig struct {
	Provider      string
	APIKey        string
	Topic         string
	Depth         ResearchDepth
	ModelCall     ModelCallFunc
	Progress      chan<- string // status updates for the TUI
	ContextBudget int           // max chars for accumulated findings (0 = default 16000)
}

// RunResearch executes the multi-round research pipeline. It searches,
// fetches pages, asks the model to analyse findings, and extracts follow-up
// queries for subsequent rounds.
func RunResearch(ctx context.Context, cfg ResearchConfig) (ResearchResult, error) {
	maxRounds := int(cfg.Depth)
	var allFindings strings.Builder
	var sources []ResearchSource
	query := cfg.Topic

	contextBudget := cfg.ContextBudget
	if contextBudget <= 0 {
		contextBudget = 16000 // sensible default
	}
	// Per-round budget: reserve some space for headings and leave room for all rounds.
	perRoundBudget := (contextBudget - 500) / maxRounds
	if perRoundBudget < 1000 {
		perRoundBudget = 1000
	}

	completedRounds := 0
	for round := 1; round <= maxRounds; round++ {
		select {
		case <-ctx.Done():
			// Timeout — return partial findings if we have any
			if allFindings.Len() > 0 {
				sendProgress(ctx, cfg.Progress, fmt.Sprintf("Timeout after %d rounds — compiling partial results...", completedRounds))
				return ResearchResult{Topic: cfg.Topic, AccumulatedContext: allFindings.String(), Sources: sources, Rounds: completedRounds}, nil
			}
			return ResearchResult{}, fmt.Errorf("research timed out with no results for %q", query)
		default:
		}

		// --- search ---
		sendProgress(ctx, cfg.Progress, fmt.Sprintf("Round %d/%d: searching %q...", round, maxRounds, query))

		results, err := QueryResults(cfg.Provider, cfg.APIKey, query)
		if err != nil || len(results) == 0 {
			if round == 1 && len(sources) == 0 {
				// First round with zero results is fatal.
				return ResearchResult{}, fmt.Errorf("search returned no results for %q", query)
			}
			continue // skip this round
		}

		// --- fetch pages ---
		sendProgress(ctx, cfg.Progress, fmt.Sprintf("Round %d/%d: reading pages...", round, maxRounds))

		var roundContent strings.Builder
		roundContent.WriteString(formatResults(results))

		fetched := 0
		for i, r := range results {
			if fetched >= maxDeepFetch || roundContent.Len() > contentTarget {
				break
			}
			content, ferr := FetchContent(r.URL)
			if ferr != nil || len(strings.TrimSpace(content)) < minPageContent {
				continue
			}
			fetched++
			fmt.Fprintf(&roundContent, "\n\n--- Content from [%d] %s ---\n\n%s", i+1, r.URL, content)

			sources = append(sources, ResearchSource{
				Title: r.Title,
				URL:   r.URL,
				Round: round,
			})
		}

		// --- model analysis (blocking) ---
		sendProgress(ctx, cfg.Progress, fmt.Sprintf("Round %d/%d: analysing findings...", round, maxRounds))

		analysisPrompt := fmt.Sprintf(
			"You are a research analyst. The user is researching: %s\n\n"+
				"This is round %d of %d.\n\n"+
				"Here are the search results and page content for this round:\n\n%s\n\n"+
				"Previous findings so far:\n%s\n\n"+
				"Instructions:\n"+
				"1. Summarise the KEY findings from this round's content. Be specific — include facts, numbers, and quotes.\n"+
				"2. Identify knowledge gaps that still need investigation.\n"+
				"3. Output 2-3 follow-up search queries (one per line) prefixed with QUERY: that would fill those gaps.\n"+
				"   Example: QUERY: latest benchmarks comparing Rust and Go web frameworks 2024\n"+
				"4. Do NOT repeat information already covered in previous findings.",
			cfg.Topic, round, maxRounds,
			compactContent(roundContent.String(), 6000),
			compactContent(allFindings.String(), 2000),
		)

		analysis, merr := cfg.ModelCall(ctx, analysisPrompt)
		if merr != nil {
			// Context cancelled mid-analysis — save raw content and stop gracefully
			if ctx.Err() != nil {
				allFindings.WriteString(fmt.Sprintf("\n\n## Round %d\n\n%s", round, compactContent(roundContent.String(), perRoundBudget)))
				completedRounds = round
				sendProgress(ctx, cfg.Progress, fmt.Sprintf("Timeout during round %d — compiling partial results...", round))
				break
			}
			// Other model error — keep raw content, continue to next round
			allFindings.WriteString(fmt.Sprintf("\n\n## Round %d\n\n%s", round, compactContent(roundContent.String(), perRoundBudget)))
		} else {
			allFindings.WriteString(fmt.Sprintf("\n\n## Round %d\n\n%s", round, compactContent(analysis, perRoundBudget)))
			// Extract next query for the following round
			if nextQueries := extractFollowUpQueries(analysis); len(nextQueries) > 0 {
				query = nextQueries[0]
			}
		}
		completedRounds = round

		// Hard stop if we've already filled the budget
		if allFindings.Len() >= contextBudget {
			break
		}
	}

	if allFindings.Len() == 0 {
		return ResearchResult{}, fmt.Errorf("research produced no findings for %q", cfg.Topic)
	}

	return ResearchResult{
		Topic:              cfg.Topic,
		AccumulatedContext: allFindings.String(),
		Sources:            sources,
		Rounds:             completedRounds,
	}, nil
}

// extractFollowUpQueries parses lines prefixed with "QUERY:" from model output.
func extractFollowUpQueries(text string) []string {
	var queries []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "QUERY:") {
			q := strings.TrimSpace(line[len("QUERY:"):])
			if q != "" {
				queries = append(queries, q)
			}
		}
	}
	return queries
}

// TruncateContent truncates s to at most maxChars, cutting at the last newline
// before the limit to avoid breaking mid-sentence. Exported for use by the TUI
// when pre-flight checking context budget.
func TruncateContent(s string, maxChars int) string {
	return compactContent(s, maxChars)
}

// compactContent truncates s to at most maxChars, cutting at the last newline
// before the limit to avoid breaking mid-sentence.
func compactContent(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	cut := s[:maxChars]
	if idx := strings.LastIndex(cut, "\n"); idx > maxChars/2 {
		cut = cut[:idx]
	}
	return cut + "\n... (truncated)"
}
