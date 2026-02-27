// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// tavilySearch queries the Tavily API and returns structured results.
func tavilySearch(apiKey, query string) ([]SearchResult, error) {
	endpoint := "https://api.tavily.com/search"

	body, err := json.Marshal(map[string]any{
		"api_key":     apiKey,
		"query":       query,
		"max_results": 5,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := searchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching Tavily results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("Tavily API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("parsing Tavily response: %w", err)
	}

	var results []SearchResult
	for _, r := range data.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}

	return results, nil
}
