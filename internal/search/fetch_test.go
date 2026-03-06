package search

import (
	"testing"
)

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // expected substring in output
	}{
		{
			name:  "plain text preserved",
			input: "<p>Hello World</p>",
			want:  "Hello World",
		},
		{
			name:  "script tags removed",
			input: "<script>var x = 1;</script><p>Content</p>",
			want:  "Content",
		},
		{
			name:  "style tags removed",
			input: "<style>.foo { color: red; }</style><p>Visible</p>",
			want:  "Visible",
		},
		{
			name:  "noscript removed",
			input: "<noscript>Enable JS</noscript><p>Text</p>",
			want:  "Text",
		},
		{
			name:  "nested elements",
			input: "<div><p>Inner <strong>bold</strong> text</p></div>",
			want:  "Inner bold text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			if !containsSubstring(got, tt.want) {
				t.Errorf("stripHTML output %q does not contain %q", got, tt.want)
			}
		})
	}
}

func TestStripHTML_ScriptNotInOutput(t *testing.T) {
	html := `<html><head><script>alert("xss")</script></head><body><p>Safe content</p></body></html>`
	result := stripHTML(html)
	if containsSubstring(result, "alert") {
		t.Error("script content should be stripped from output")
	}
	if !containsSubstring(result, "Safe content") {
		t.Error("body content should be preserved")
	}
}

func TestStripHTML_LinkPreservation(t *testing.T) {
	html := `<a href="https://example.com/article/details">Read more</a>`
	result := stripHTML(html)
	if !containsSubstring(result, "Read more") {
		t.Error("link text should be preserved")
	}
	if !containsSubstring(result, "https://example.com/article/details") {
		t.Error("article link URL should be preserved inline")
	}
}

func TestIsArticleLink(t *testing.T) {
	tests := []struct {
		href string
		want bool
	}{
		{"https://example.com/blog/post-123", true},
		{"https://example.com/docs/guide/setup", true},
		{"https://example.com", false},       // homepage
		{"https://example.com/", false},      // homepage with slash
		{"https://example.com/about", false}, // single path segment
		{"#section", false},                  // anchor
		{"javascript:void(0)", false},        // javascript
		{"", false},                          // empty
		{"/relative/path", false},            // relative URL
		{"ftp://example.com/file", false},    // not http/https
	}

	for _, tt := range tests {
		t.Run(tt.href, func(t *testing.T) {
			got := isArticleLink(tt.href)
			if got != tt.want {
				t.Errorf("isArticleLink(%q) = %v, want %v", tt.href, got, tt.want)
			}
		})
	}
}

func TestIsBlockElement(t *testing.T) {
	block := []string{"p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6",
		"li", "tr", "td", "th", "blockquote", "pre", "hr", "section", "article"}
	for _, tag := range block {
		if !isBlockElement(tag) {
			t.Errorf("isBlockElement(%q) = false, want true", tag)
		}
	}

	inline := []string{"span", "a", "strong", "em", "b", "i", "code", "img"}
	for _, tag := range inline {
		if isBlockElement(tag) {
			t.Errorf("isBlockElement(%q) = true, want false", tag)
		}
	}
}

func TestLooksLikeCSS(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{".class{color: red;}", true},
		{"div { margin: 0; }", true},
		{"background: url('image.png')", true},
		{`background: url("image.png")`, true},
		{"--wpr-bg: blue", true},
		{"--wp-primary: red", true},
		{"This is normal text", false},
		{"Hello world", false},
		{"func main() { fmt.Println() }", false}, // code with braces but many words
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := looksLikeCSS(tt.line)
			if got != tt.want {
				t.Errorf("looksLikeCSS(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestCollapseWhitespace(t *testing.T) {
	input := "  Hello  \n\n  World  \n  \n  Foo  "
	result := collapseWhitespace(input)
	if !containsSubstring(result, "Hello") {
		t.Error("should contain Hello")
	}
	if !containsSubstring(result, "World") {
		t.Error("should contain World")
	}
	if !containsSubstring(result, "Foo") {
		t.Error("should contain Foo")
	}
}

func TestCollapseWhitespace_FiltersCSS(t *testing.T) {
	input := "Real content\n.class{color: red;}\nMore content"
	result := collapseWhitespace(input)
	if containsSubstring(result, "color") {
		t.Error("CSS should be filtered out")
	}
	if !containsSubstring(result, "Real content") {
		t.Error("real content should be preserved")
	}
}

func TestFetchMaxConstants(t *testing.T) {
	if fetchMaxBody != 1<<20 {
		t.Errorf("fetchMaxBody = %d, want %d", fetchMaxBody, 1<<20)
	}
	if fetchMaxChars != 8000 {
		t.Errorf("fetchMaxChars = %d, want 8000", fetchMaxChars)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
