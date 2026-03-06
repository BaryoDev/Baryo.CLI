package rag

import (
	"testing"
)

func TestNewBM25Index(t *testing.T) {
	idx := NewBM25Index()
	if idx == nil {
		t.Fatal("NewBM25Index returned nil")
	}
}

func TestBM25Index_AddAndQuery(t *testing.T) {
	idx := NewBM25Index()
	idx.Add([]Chunk{
		{Source: "a.go", Text: "func main() { fmt.Println(hello world) }"},
		{Source: "b.go", Text: "package config loading yaml configuration"},
		{Source: "c.go", Text: "HTTP server handles requests and responses"},
	})

	results := idx.Query("yaml configuration", 2)
	if len(results) == 0 {
		t.Fatal("Query returned no results")
	}
	if results[0].Source != "b.go" {
		t.Errorf("top result source = %q, want %q", results[0].Source, "b.go")
	}
	if results[0].Score <= 0 {
		t.Error("top result should have positive score")
	}
}

func TestBM25Index_QueryEmpty(t *testing.T) {
	idx := NewBM25Index()
	results := idx.Query("hello", 5)
	if results != nil {
		t.Errorf("Query on empty index should return nil, got %v", results)
	}
}

func TestBM25Index_QueryZeroTopK(t *testing.T) {
	idx := NewBM25Index()
	idx.Add([]Chunk{{Source: "a.go", Text: "hello world"}})
	results := idx.Query("hello", 0)
	if results != nil {
		t.Errorf("Query with topK=0 should return nil, got %v", results)
	}
}

func TestBM25Index_QueryNoMatch(t *testing.T) {
	idx := NewBM25Index()
	idx.Add([]Chunk{
		{Source: "a.go", Text: "func main() { fmt.Println(hello) }"},
	})
	results := idx.Query("xyzzynonexistent", 5)
	if len(results) != 0 {
		t.Errorf("Query with no matching terms should return empty, got %d results", len(results))
	}
}

func TestBM25Index_QueryStopwordsOnly(t *testing.T) {
	idx := NewBM25Index()
	idx.Add([]Chunk{
		{Source: "a.go", Text: "func main() {}"},
	})
	results := idx.Query("the and is", 5)
	if results != nil {
		t.Errorf("Query with only stopwords should return nil, got %v", results)
	}
}

func TestBM25Index_TopKClamp(t *testing.T) {
	idx := NewBM25Index()
	idx.Add([]Chunk{
		{Source: "a.go", Text: "hello world"},
		{Source: "b.go", Text: "hello there"},
	})
	results := idx.Query("hello", 10)
	if len(results) > 2 {
		t.Errorf("returned %d results, should not exceed number of matching chunks (2)", len(results))
	}
}

func TestBM25Index_Ordering(t *testing.T) {
	idx := NewBM25Index()
	idx.Add([]Chunk{
		{Source: "irrelevant.go", Text: "this file is about something else entirely unrelated topic"},
		{Source: "relevant.go", Text: "database connection pool database query database migration"},
		{Source: "also_relevant.go", Text: "database query handler"},
	})

	results := idx.Query("database query", 3)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// Both database-related chunks should rank above the irrelevant one
	relevantSources := map[string]bool{"relevant.go": true, "also_relevant.go": true}
	for i := 0; i < 2 && i < len(results); i++ {
		if !relevantSources[results[i].Source] {
			t.Errorf("results[%d].Source = %q, expected a relevant source", i, results[i].Source)
		}
	}

	// Scores should be in descending order
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not in descending order: [%d].Score=%f > [%d].Score=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  int // minimum expected tokens
	}{
		{"hello world", 2},
		{"The quick brown fox", 2}, // "the" is a stopword, "fox" is 3 chars
		{"a b c", 0},               // all single-char tokens filtered
		{"", 0},
		{"func main() {}", 2}, // "func" and "main"
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens := tokenize(tt.input)
			if len(tokens) < tt.want {
				t.Errorf("tokenize(%q) returned %d tokens, want >= %d: %v",
					tt.input, len(tokens), tt.want, tokens)
			}
		})
	}
}

func TestTokenize_StopwordsFiltered(t *testing.T) {
	tokens := tokenize("the is and are")
	if len(tokens) != 0 {
		t.Errorf("all stopwords should be filtered, got %v", tokens)
	}
}

func TestTokenize_CaseInsensitive(t *testing.T) {
	upper := tokenize("Hello World")
	lower := tokenize("hello world")
	if len(upper) != len(lower) {
		t.Errorf("case should not affect token count: upper=%d, lower=%d", len(upper), len(lower))
	}
}

func TestTokenize_ShortTokensFiltered(t *testing.T) {
	tokens := tokenize("a b c d")
	if len(tokens) != 0 {
		t.Errorf("single-char tokens should be filtered, got %v", tokens)
	}
}
