package mcp

import "testing"

func boolPtr(b bool) *bool { return &b }

func trustManager() *Manager {
	m := NewManager()
	m.clients = map[string]*Client{
		"search": {name: "search", tools: []MCPToolDef{
			{Name: "query", Annotations: &ToolAnnotations{ReadOnlyHint: boolPtr(true)}},
			{Name: "cache_clear", Annotations: &ToolAnnotations{ReadOnlyHint: boolPtr(false)}},
			{Name: "unannotated"},
		}},
		"fs": {name: "fs", tools: []MCPToolDef{{Name: "write"}, {Name: "read"}}},
	}
	m.toolMap = map[string]string{
		"mcp__search__query":       "search",
		"mcp__search__cache_clear": "search",
		"mcp__search__unannotated": "search",
		"mcp__fs__write":           "fs",
		"mcp__fs__read":            "fs",
	}
	m.trust = map[string]string{"fs": ""}
	return m
}

func TestIsReadOnlyToolFromAnnotation(t *testing.T) {
	if !trustManager().IsReadOnlyTool("mcp__search__query") {
		t.Error("readOnlyHint true should mark the tool read-only")
	}
}

func TestIsReadOnlyToolRejectsFalseAnnotation(t *testing.T) {
	if trustManager().IsReadOnlyTool("mcp__search__cache_clear") {
		t.Error("readOnlyHint false must not be read-only")
	}
}

// Most servers ship no annotations at all. Those must be gated, not trusted.
func TestIsReadOnlyToolFailsClosedWithoutAnnotation(t *testing.T) {
	if trustManager().IsReadOnlyTool("mcp__search__unannotated") {
		t.Error("a tool with no annotations must not be treated as read-only")
	}
}

// The per-server override exists so a whole server can be marked read-only once
// instead of approving every call from a server that ships no annotations.
func TestIsReadOnlyToolFromServerTrust(t *testing.T) {
	m := trustManager()
	m.trust["fs"] = TrustReadOnly
	if !m.IsReadOnlyTool("mcp__fs__write") {
		t.Error("a server marked read-only in config should mark its tools read-only")
	}
	if m.IsReadOnlyTool("mcp__search__cache_clear") {
		t.Error("trusting one server must not affect another")
	}
}

func TestIsReadOnlyToolUnknownName(t *testing.T) {
	if trustManager().IsReadOnlyTool("mcp__nope__nope") {
		t.Error("an unknown qualified name must not be read-only")
	}
}

func TestServerTrustParsedFromConfig(t *testing.T) {
	m := NewManager()
	m.applyTrust([]ServerConfig{
		{Name: "search", Trust: "read-only"},
		{Name: "fs"},
	})
	if m.trust["search"] != TrustReadOnly {
		t.Errorf("search trust = %q, want %q", m.trust["search"], TrustReadOnly)
	}
	if m.trust["fs"] == TrustReadOnly {
		t.Error("fs was not marked read-only")
	}
}
