package tools

import (
	"bytes"
	"encoding/json"
	"sort"
	"testing"
)

// Tool definitions are rendered into the prompt right after the system text by
// most chat templates. Local inference servers only reuse their KV cache while
// the prompt prefix is byte-identical, so an unstable tool order re-prefills the
// tools block and the whole conversation on every turn.

func TestDockerDefinitionsOrderIsStable(t *testing.T) {
	first := dockerToolNames()
	for i := 0; i < 20; i++ {
		got := dockerToolNames()
		if !slicesEqual(first, got) {
			t.Fatalf("order changed on call %d:\nfirst: %v\ngot:   %v", i+2, first, got)
		}
	}
}

func TestAllDefinitionsSortedByName(t *testing.T) {
	assertSorted(t, "AllDefinitions", definitionNames(AllDefinitions()))
}

func TestReadOnlyDefinitionsSortedByName(t *testing.T) {
	assertSorted(t, "ReadOnlyDefinitions", definitionNames(ReadOnlyDefinitions()))
}

func TestNamesSorted(t *testing.T) {
	assertSorted(t, "Names", Names())
}

func definitionNames(defs []Definition) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

func dockerToolNames() []string {
	names := make([]string, 0, len(registry))
	for _, d := range DockerDefinitions() {
		names = append(names, d.Function.Name)
	}
	return names
}

func assertSorted(t *testing.T, what string, names []string) {
	t.Helper()
	if len(names) < 2 {
		t.Fatalf("%s returned %d entries, expected the registry to be populated", what, len(names))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("%s is not sorted by name: %v", what, names)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The property that actually matters is byte-level: a local inference server
// reuses its KV cache only while the serialized prefix is identical, and the
// tools array is part of that prefix.
func TestDockerDefinitionsSerializeIdentically(t *testing.T) {
	first, err := json.Marshal(DockerDefinitions())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := json.Marshal(DockerDefinitions())
		if err != nil {
			t.Fatalf("marshal on call %d: %v", i+2, err)
		}
		if !bytes.Equal(first, got) {
			t.Fatalf("serialized tools differ on call %d (%d vs %d bytes)", i+2, len(first), len(got))
		}
	}
	t.Logf("tools array is %d bytes, stable across 21 calls", len(first))
}
