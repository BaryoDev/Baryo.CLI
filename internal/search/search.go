// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package search

import (
	"fmt"
	"strings"
)

const (
	maxDeepFetch   = 3    // fetch at most 3 URLs
	contentTarget  = 6000 // stop once we have enough content
	minPageContent = 200  // skip pages with less than this
)

// SearchResult holds a single web search result.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// QueryResults dispatches a web search to the configured provider and returns
// structured results.
func QueryResults(provider, apiKey, query string) ([]SearchResult, error) {
	switch strings.ToLower(provider) {
	case "brave":
		if apiKey == "" {
			return nil, fmt.Errorf("brave search requires an API key (set search_api_key or BARYO_SEARCH_API_KEY)")
		}
		return braveSearch(apiKey, query)
	case "tavily":
		if apiKey == "" {
			return nil, fmt.Errorf("tavily search requires an API key (set search_api_key or BARYO_SEARCH_API_KEY)")
		}
		return tavilySearch(apiKey, query)
	case "", "duckduckgo":
		return duckduckgoSearch(query)
	default:
		return nil, fmt.Errorf("unknown search provider %q (available: duckduckgo, brave, tavily)", provider)
	}
}

// Query dispatches a web search to the configured provider and returns
// formatted results suitable for injecting into the conversation.
func Query(provider, apiKey, query string) (string, error) {
	results, err := QueryResults(provider, apiKey, query)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No results found.", nil
	}
	return formatResults(results), nil
}

// DeepQuery searches and then auto-fetches top result pages for actual content.
// It stops fetching once enough content has been accumulated.
func DeepQuery(provider, apiKey, query string) (string, error) {
	results, err := QueryResults(provider, apiKey, query)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No results found.", nil
	}

	var b strings.Builder
	b.WriteString(formatResults(results))

	fetched := 0
	for i, r := range results {
		if fetched >= maxDeepFetch || b.Len() > contentTarget {
			break
		}
		content, err := FetchContent(r.URL)
		if err != nil || len(strings.TrimSpace(content)) < minPageContent {
			continue
		}
		fetched++
		fmt.Fprintf(&b, "\n\n--- Content from [%d] %s ---\n\n%s", i+1, r.URL, content)
	}

	return b.String(), nil
}

// formatResults converts a slice of SearchResult into a readable string.
func formatResults(results []SearchResult) string {
	if len(results) == 0 {
		return "No results found."
	}
	var b strings.Builder
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Snippet))
	}
	return strings.TrimSpace(b.String())
}
