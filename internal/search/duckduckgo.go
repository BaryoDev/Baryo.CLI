// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package search

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// duckduckgoSearch queries DuckDuckGo's HTML endpoint and extracts results.
func duckduckgoSearch(query string) ([]SearchResult, error) {
	endpoint := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Baryo/1.0)")

	resp, err := searchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching DuckDuckGo results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return extractDDGResults(doc), nil
}

// extractDDGResults walks the HTML tree and extracts search results.
// DuckDuckGo HTML results use class "result__a" for title links and
// "result__snippet" for snippets.
func extractDDGResults(doc *html.Node) []SearchResult {
	var results []SearchResult
	var walk func(*html.Node)

	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "result") {
			r := extractSingleResult(n)
			if r.Title != "" && r.URL != "" {
				results = append(results, r)
			}
		}
		if len(results) >= 5 {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if len(results) >= 5 {
				return
			}
		}
	}
	walk(doc)
	return results
}

// extractSingleResult extracts title, URL, and snippet from a result div.
func extractSingleResult(n *html.Node) SearchResult {
	var r SearchResult
	var walk func(*html.Node)

	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "a" && hasClass(n, "result__a") {
				r.Title = textContent(n)
				for _, a := range n.Attr {
					if a.Key == "href" {
						r.URL = extractDDGURL(a.Val)
					}
				}
			}
			if hasClass(n, "result__snippet") {
				r.Snippet = textContent(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return r
}

// extractDDGURL extracts the actual URL from DuckDuckGo's redirect URL.
func extractDDGURL(href string) string {
	// DDG wraps URLs in a redirect: //duckduckgo.com/l/?uddg=<encoded>&...
	if strings.Contains(href, "uddg=") {
		if u, err := url.Parse(href); err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	// Already a direct URL
	if strings.HasPrefix(href, "http") {
		return href
	}
	return "https:" + href
}

// hasClass checks if an HTML node has a specific class.
func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

// textContent returns the concatenated text content of a node and its children.
func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}
