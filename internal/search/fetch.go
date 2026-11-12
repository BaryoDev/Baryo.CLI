// SPDX-License-Identifier: MIT

package search

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

const (
	fetchMaxBody  = 1 << 20 // 1 MB
	fetchMaxChars = 8000    // truncate extracted text
)

// Package-level HTTP clients (reused across calls to avoid per-call overhead).
var (
	fetchClient  = newFetchClient(nil)
	searchClient = &http.Client{Timeout: 15 * time.Second}
)

// blockedPrefixes are ranges that are not global unicast in practice or that
// can reach internal hosts. Anything not global unicast is refused as well.
var blockedPrefixes = []struct {
	prefix netip.Prefix
	why    string
}{
	{netip.MustParsePrefix("100.64.0.0/10"), "carrier-grade NAT"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmark"},
	{netip.MustParsePrefix("192.0.0.0/24"), "reserved"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved"},
}

// Prefixes that embed an IPv4 address; the embedded address is checked too.
var (
	nat64      = netip.MustParsePrefix("64:ff9b::/96")
	nat64Local = netip.MustParsePrefix("64:ff9b:1::/48")
	sixToFour  = netip.MustParsePrefix("2002::/16")
	v4Compat   = netip.MustParsePrefix("::/96")
)

// blockedAddr reports why an address must not be fetched, or "" if it may be.
// It denies by default: only global unicast addresses pass. Fetched pages are
// untrusted input, so fetch_page must not reach services on this machine or
// its network.
func blockedAddr(a netip.Addr) string {
	a = a.Unmap()
	if a.Is6() {
		raw := a.As16()
		var emb netip.Addr
		switch {
		case nat64.Contains(a) || v4Compat.Contains(a):
			emb = netip.AddrFrom4([4]byte{raw[12], raw[13], raw[14], raw[15]})
		case nat64Local.Contains(a):
			// RFC 8215 local-use prefix has no fixed embedding; refuse it all.
			return "NAT64"
		case sixToFour.Contains(a):
			emb = netip.AddrFrom4([4]byte{raw[2], raw[3], raw[4], raw[5]})
		}
		if emb.IsValid() {
			if why := blockedAddr(emb); why != "" {
				return why + " (embedded)"
			}
			if v4Compat.Contains(a) {
				return "IPv4-compatible"
			}
			return ""
		}
	}
	switch {
	case a.IsLoopback():
		return "loopback"
	case a.IsUnspecified():
		return "unspecified"
	case a.IsPrivate():
		return "private"
	case a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast():
		return "link-local"
	case a.IsMulticast(), a.IsInterfaceLocalMulticast():
		return "multicast"
	case a.Is4() && a.As4()[0] == 0:
		return "unspecified"
	}
	for _, b := range blockedPrefixes {
		if b.prefix.Contains(a) {
			return b.why
		}
	}
	if !a.IsGlobalUnicast() {
		return "not global unicast"
	}
	return ""
}

// newFetchClient builds the page-fetch client. The check runs in the dialer's
// Control hook, which sees the address after DNS resolution and runs again for
// every redirect hop, so a hostname that resolves to a private address or a
// redirect to one is refused. allow, when set, lets a test exempt one
// "host:port" it needs to reach. Proxies are not used, since a proxy would
// hide the real destination from this check.
func newFetchClient(allow func(hostport string) bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if allow != nil && allow(address) {
				return nil
			}
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("refusing to connect to %q: cannot parse address", address)
			}
			if why := blockedAddr(ap.Addr()); why != "" {
				return fmt.Errorf("refusing to connect to %s address %s", why, ap.Addr())
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}
}

// FetchContent downloads a URL and returns the extracted text content only.
// Uses a shorter timeout suitable for batch fetching multiple pages.
func FetchContent(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http/https URLs are supported")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Baryo/1.0)")

	resp, err := fetchClient.Do(req)
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
func Fetch(ctx context.Context, rawURL string) (string, error) {
	text, err := FetchContent(ctx, rawURL)
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
