package tui

import (
	"strings"
	"testing"
)

// Pinned files are injected into the system prompt. Map iteration order would
// reshuffle that block on every turn and break KV cache reuse on local models.

func pinnedModel() *ChatModel {
	return &ChatModel{pinnedFiles: map[string]string{
		"/tmp/zulu.go":  "z",
		"/tmp/alpha.go": "a",
		"/tmp/mike.go":  "m",
		"/tmp/bravo.go": "b",
		"/tmp/kilo.go":  "k",
	}}
}

func pinnedPaths(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "[/tmp/") && strings.HasSuffix(line, "]") {
			paths = append(paths, strings.Trim(line, "[]"))
		}
	}
	return paths
}

func TestBuildPinnedContextOrderIsStable(t *testing.T) {
	m := pinnedModel()
	first := pinnedPaths(m.buildPinnedContext())
	if len(first) != 5 {
		t.Fatalf("expected 5 pinned paths, got %d: %v", len(first), first)
	}
	for i := 0; i < 20; i++ {
		got := pinnedPaths(m.buildPinnedContext())
		for j := range first {
			if first[j] != got[j] {
				t.Fatalf("order changed on call %d:\nfirst: %v\ngot:   %v", i+2, first, got)
			}
		}
	}
}

func TestBuildPinnedContextSortedByPath(t *testing.T) {
	got := pinnedPaths(pinnedModel().buildPinnedContext())
	want := []string{"/tmp/alpha.go", "/tmp/bravo.go", "/tmp/kilo.go", "/tmp/mike.go", "/tmp/zulu.go"}
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
}
