// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package search

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	fetchMaxBody  = 1 << 20 // 1 MB
	fetchMaxChars = 8000    // truncate extracted text
)

// FetchContent downloads a URL and returns the extracted text content only.
// Uses a shorter timeout suitable for batch fetching multiple pages.
func FetchContent(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http/https URLs are supported")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Baryo/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBody))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	text := stripHTML(string(body))
	if len(text) > fetchMaxChars {
		text = text[:fetchMaxChars] + "\n... (truncated)"
	}

	return text, nil
}

// Fetch downloads a URL and returns its text content with HTML tags stripped.
// The result includes a "Content from <url>:" prefix for display.
func Fetch(rawURL string) (string, error) {
	text, err := FetchContent(rawURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Content from %s:\n\n%s", rawURL, text), nil
}

// stripHTML removes HTML tags and returns the text content.
// Links are preserved inline as "text [url]" so article URLs survive extraction.
func stripHTML(s string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	var skip bool
	var skipDepth int
	var pendingHref string // href from current <a> tag

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return collapseWhitespace(b.String())
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			// Skip non-content elements
			if tag == "script" || tag == "style" || tag == "noscript" || tag == "svg" {
				skip = true
				skipDepth++
			}
			// Capture article links
			if tag == "a" && !skip {
				for {
					key, val, more := tokenizer.TagAttr()
					if string(key) == "href" {
						href := string(val)
						if isArticleLink(href) {
							pendingHref = href
						}
					}
					if !more {
						break
					}
				}
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "script" || tag == "style" || tag == "noscript" || tag == "svg" {
				skipDepth--
				if skipDepth <= 0 {
					skip = false
					skipDepth = 0
				}
			}
			if tag == "a" && pendingHref != "" {
				fmt.Fprintf(&b, " [%s]", pendingHref)
				pendingHref = ""
			}
			// Add newline after block elements
			if isBlockElement(tag) {
				b.WriteString("\n")
			}
		case html.TextToken:
			if !skip {
				b.WriteString(tokenizer.Token().Data)
			}
		}
	}
}

// isArticleLink returns true if a URL looks like a specific article/page
// rather than a homepage, anchor, or javascript link.
func isArticleLink(href string) bool {
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
		return false
	}
	// Must be an absolute http(s) URL
	if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
		return false
	}
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	// Must have a meaningful path (not just "/" or "/category")
	path := strings.Trim(u.Path, "/")
	return strings.Contains(path, "/")
}

// isBlockElement returns true for HTML elements that typically produce line breaks.
func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6",
		"li", "tr", "td", "th", "blockquote", "pre", "hr", "section", "article":
		return true
	}
	return false
}

// collapseWhitespace reduces runs of whitespace to single spaces, preserving newlines.
// Also filters out lines that look like leaked CSS.
func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !looksLikeCSS(trimmed) {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}

// looksLikeCSS returns true if a line appears to be leaked CSS rather than content.
func looksLikeCSS(line string) bool {
	// CSS rule blocks: selectors with { } and : (e.g. ".class{prop: val;}")
	if strings.Contains(line, "{") && strings.Contains(line, "}") && strings.Contains(line, ":") {
		// Allow lines that have significant text outside braces
		before := line[:strings.Index(line, "{")]
		if len(strings.Fields(before)) <= 2 {
			return true
		}
	}
	// CSS url() references
	if strings.Contains(line, "url('") || strings.Contains(line, "url(\"") {
		return true
	}
	// CSS custom properties
	if strings.Contains(line, "--wpr-bg") || strings.Contains(line, "--wp-") {
		return true
	}
	return false
}
