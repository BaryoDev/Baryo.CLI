// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"testing"
)

func TestNormalizeWhitespace(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"tabs to spaces", "func\tfoo()", "func foo()"},
		{"leading spaces", "    hello world", "hello world"},
		{"mixed whitespace", "  func  \t  bar()  ", "func bar()"},
		{"multiline", "  hello\n\tworld", "hello\nworld"},
		{"already normalized", "hello world", "hello world"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeWhitespace(tc.input)
			if got != tc.want {
				t.Errorf("normalizeWhitespace(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFuzzyFind(t *testing.T) {
	content := "package main\n\nfunc foo() {\n\treturn\n}\n\nfunc bar() {\n\treturn\n}"

	cases := []struct {
		name      string
		oldString string
		wantMatch string
		wantFound bool
	}{
		{
			name:      "exact match",
			oldString: "func foo() {\n\treturn\n}",
			wantMatch: "func foo() {\n\treturn\n}",
			wantFound: true,
		},
		{
			name:      "whitespace difference",
			oldString: "func foo() {\n    return\n}",
			wantMatch: "func foo() {\n\treturn\n}",
			wantFound: true,
		},
		{
			name:      "no match",
			oldString: "func baz() {\n\treturn\n}",
			wantMatch: "",
			wantFound: false,
		},
		{
			name:      "ambiguous match",
			oldString: "return",
			wantMatch: "",
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match, found := fuzzyFind(content, tc.oldString)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if match != tc.wantMatch {
				t.Errorf("match = %q, want %q", match, tc.wantMatch)
			}
		})
	}
}

func TestFuzzyFindMultipleAmbiguous(t *testing.T) {
	// Two functions with identical structure (only whitespace differs).
	content := "func a() {\n\treturn\n}\n\nfunc b() {\n\treturn\n}"
	_, found := fuzzyFind(content, "return")
	if found {
		t.Error("expected no match for ambiguous fuzzy search")
	}
}
