// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

import "testing"

func TestToolCallKeyDistinguishesArgs(t *testing.T) {
	// Different paths must produce different keys so repeat calls
	// with new arguments are not deduplicated away.
	a := toolCallKey("read_file", `{"path":"a.go"}`)
	b := toolCallKey("read_file", `{"path":"b.go"}`)
	if a == b {
		t.Errorf("read_file keys collide: %q", a)
	}

	g1 := toolCallKey("grep", `{"pattern":"foo"}`)
	g2 := toolCallKey("grep", `{"pattern":"bar"}`)
	if g1 == g2 {
		t.Errorf("grep keys collide: %q", g1)
	}

	s1 := toolCallKey("shell", `{"command":"ls"}`)
	s2 := toolCallKey("shell", `{"command":"pwd"}`)
	if s1 == s2 {
		t.Errorf("shell keys collide: %q", s1)
	}
}

func TestToolCallKeyIdenticalArgsMatch(t *testing.T) {
	// Key order in the JSON must not matter.
	a := toolCallKey("edit_file", `{"path":"a.go","new_string":"x"}`)
	b := toolCallKey("edit_file", `{"new_string":"x","path":"a.go"}`)
	if a != b {
		t.Errorf("equivalent args produce different keys: %q vs %q", a, b)
	}
}

func TestToolCallKeySearchNamespace(t *testing.T) {
	// Search-family tools share a namespace keyed on the subject.
	a := toolCallKey("web_search", `{"query":"Go generics"}`)
	b := toolCallKey("deep_research", `{"topic":"go generics"}`)
	if a != b {
		t.Errorf("search-family keys differ: %q vs %q", a, b)
	}
}
