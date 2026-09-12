package mcp

import (
	"sort"
	"strings"
	"testing"
)

// CompactToolDefinitions feeds the prompt's tools block, which chat templates
// render into the prefix. An unstable order costs a full re-prefill per turn on
// a local model, so the order must not depend on map iteration.

func fakeManager() *Manager {
	m := NewManager()
	m.clients = map[string]*Client{
		"zulu":   {name: "zulu", tools: []MCPToolDef{{Name: "z2"}, {Name: "z1"}}},
		"alpha":  {name: "alpha", tools: []MCPToolDef{{Name: "a1"}, {Name: "a2"}}},
		"mike":   {name: "mike", tools: []MCPToolDef{{Name: "m1"}}},
		"bravo":  {name: "bravo", tools: []MCPToolDef{{Name: "b1"}}},
		"yankee": {name: "yankee", tools: []MCPToolDef{{Name: "y1"}}},
	}
	return m
}

func defNames(m *Manager) []string {
	defs := m.CompactToolDefinitions(nil, 32000)
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

func TestCompactToolDefinitionsOrderIsStable(t *testing.T) {
	m := fakeManager()
	first := defNames(m)
	if len(first) != 7 {
		t.Fatalf("expected 7 definitions, got %d: %v", len(first), first)
	}
	for i := 0; i < 20; i++ {
		got := defNames(m)
		if len(got) != len(first) {
			t.Fatalf("length changed on call %d: %d vs %d", i+2, len(got), len(first))
		}
		for j := range first {
			if first[j] != got[j] {
				t.Fatalf("order changed on call %d:\nfirst: %v\ngot:   %v", i+2, first, got)
			}
		}
	}
}

func TestCompactToolDefinitionsGroupedBySortedServer(t *testing.T) {
	names := defNames(fakeManager())

	var servers []string
	for _, n := range names {
		server := strings.SplitN(strings.TrimPrefix(n, "mcp__"), "__", 2)[0]
		if len(servers) == 0 || servers[len(servers)-1] != server {
			servers = append(servers, server)
		}
	}
	if !sort.StringsAreSorted(servers) {
		t.Errorf("servers are not in sorted order: %v (from %v)", servers, names)
	}
	if len(servers) != 5 {
		t.Errorf("expected each server once, got %v", servers)
	}
}

// A server's own tool order comes from its tools/list response and must be
// preserved, not re-sorted.
func TestCompactToolDefinitionsPreservesServerToolOrder(t *testing.T) {
	names := defNames(fakeManager())

	var zulu []string
	for _, n := range names {
		if strings.HasPrefix(n, "mcp__zulu__") {
			zulu = append(zulu, n)
		}
	}
	want := []string{"mcp__zulu__z2", "mcp__zulu__z1"}
	if len(zulu) != 2 || zulu[0] != want[0] || zulu[1] != want[1] {
		t.Errorf("zulu tool order changed: got %v, want %v", zulu, want)
	}
}
