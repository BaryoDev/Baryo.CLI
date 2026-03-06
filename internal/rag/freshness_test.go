package rag

import (
	"fmt"
	"testing"
	"time"
)

func TestNeedsFreshInfo(t *testing.T) {
	currentYear := time.Now().Year()
	tests := []struct {
		query string
		want  bool
	}{
		// Freshness keywords
		{"what happened today in tech", true},
		{"latest version of Go", true},
		{"current version of Node.js", true},
		{"recent news about AI", true},
		{"what is happening right now", true},
		{"this week in programming", true},
		{"just released new framework", true},
		{"new release of Python", true},
		{"breaking changes in the API", true},
		{"update on the project", true},

		// Year references (with question format)
		{fmt.Sprintf("what is the best framework in %d?", currentYear), true},
		{fmt.Sprintf("what changed in %d?", currentYear-1), true},

		// Year references without question format should NOT trigger
		{fmt.Sprintf("copyright %d", currentYear), false},
		{fmt.Sprintf("version %d.0.1", currentYear), false},

		// No freshness signals
		{"how to sort a slice in Go", false},
		{"explain binary search algorithm", false},
		{"what is a goroutine", false},
		{"implement quicksort", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := NeedsFreshInfo(tt.query)
			if got != tt.want {
				t.Errorf("NeedsFreshInfo(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestLooksLikeQuestion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Question marks
		{"what is Go?", true},
		{"is this a test?", true},

		// Question words
		{"what is the best approach", true},
		{"how to implement sorting", true},
		{"when was Go released", true},
		{"which framework is best", true},
		{"who created Linux", true},
		{"where is the config file", true},

		// Intent words
		{"best Go framework for web", true},
		{"recommend a database library", true},
		{"compare React vs Vue", true},
		{"Python versus JavaScript performance", true},

		// Not questions
		{"implement the sorting algorithm", false},
		{"fix the bug in main.go", false},
		{"add error handling", false},
		{"copyright 2024 Acme Corp", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := looksLikeQuestion(tt.input)
			if got != tt.want {
				t.Errorf("looksLikeQuestion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
