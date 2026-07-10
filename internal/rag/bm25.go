// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rag

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// Chunk represents a retrievable piece of text from a source.
type Chunk struct {
	Source string  // file path or session ID
	Text   string  // chunk content
	Score  float64 // populated after Query
	Type   string  // "document" or "session"
}

// BM25Index is a thread-safe BM25 keyword search index.
type BM25Index struct {
	mu     sync.RWMutex
	chunks []Chunk
	// term → chunk index → term frequency
	tf []map[string]int
	// term → document frequency (number of chunks containing the term)
	df     map[string]int
	avgLen float64
}

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// NewBM25Index creates an empty BM25 index.
func NewBM25Index() *BM25Index {
	return &BM25Index{
		df: make(map[string]int),
	}
}

// Add indexes a batch of chunks.
func (idx *BM25Index) Add(chunks []Chunk) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, c := range chunks {
		tokens := tokenize(c.Text)
		freq := make(map[string]int, len(tokens))
		seen := make(map[string]bool, len(tokens))
		for _, t := range tokens {
			freq[t]++
			if !seen[t] {
				idx.df[t]++
				seen[t] = true
			}
		}
		idx.chunks = append(idx.chunks, c)
		idx.tf = append(idx.tf, freq)
	}

	// Recompute average document length.
	total := 0
	for _, f := range idx.tf {
		for _, count := range f {
			total += count
		}
	}
	if len(idx.chunks) > 0 {
		idx.avgLen = float64(total) / float64(len(idx.chunks))
	}
}

// Query returns the top-K chunks ranked by BM25 score for the given query.
func (idx *BM25Index) Query(query string, topK int) []Chunk {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.chunks) == 0 || topK <= 0 {
		return nil
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	n := float64(len(idx.chunks))
	type scored struct {
		idx   int
		score float64
	}
	results := make([]scored, 0, len(idx.chunks))

	for i, freq := range idx.tf {
		score := 0.0
		docLen := 0
		for _, count := range freq {
			docLen += count
		}

		for _, qt := range queryTokens {
			tfVal, ok := freq[qt]
			if !ok {
				continue
			}
			dfVal := idx.df[qt]
			if dfVal == 0 {
				continue
			}
			// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
			idf := math.Log((n-float64(dfVal)+0.5)/(float64(dfVal)+0.5) + 1.0)
			// TF normalization
			tfNorm := (float64(tfVal) * (bm25K1 + 1)) /
				(float64(tfVal) + bm25K1*(1-bm25B+bm25B*float64(docLen)/idx.avgLen))
			score += idf * tfNorm
		}
		if score > 0 {
			results = append(results, scored{idx: i, score: score})
		}
	}

	// Partial sort: find top-K by iterating and maintaining a small sorted list.
	if len(results) <= topK {
		topK = len(results)
	}
	// Simple selection: sort by score descending.
	for i := 0; i < topK; i++ {
		best := i
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[best].score {
				best = j
			}
		}
		results[i], results[best] = results[best], results[i]
	}

	out := make([]Chunk, topK)
	for i := 0; i < topK; i++ {
		c := idx.chunks[results[i].idx]
		c.Score = results[i].score
		out[i] = c
	}
	return out
}

// tokenize splits text into lowercase tokens, filtering stopwords.
func tokenize(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) < 2 || stopwords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// stopwords is a set of the top ~100 English stopwords.
var stopwords = map[string]bool{
	"the": true, "be": true, "to": true, "of": true, "and": true,
	"in": true, "that": true, "have": true, "it": true, "for": true,
	"not": true, "on": true, "with": true, "he": true, "as": true,
	"you": true, "do": true, "at": true, "this": true, "but": true,
	"his": true, "by": true, "from": true, "they": true, "we": true,
	"say": true, "her": true, "she": true, "or": true, "an": true,
	"will": true, "my": true, "one": true, "all": true, "would": true,
	"there": true, "their": true, "what": true, "so": true, "up": true,
	"out": true, "if": true, "about": true, "who": true, "get": true,
	"which": true, "go": true, "me": true, "when": true, "make": true,
	"can": true, "like": true, "time": true, "no": true, "just": true,
	"him": true, "know": true, "take": true, "people": true, "into": true,
	"year": true, "your": true, "good": true, "some": true, "could": true,
	"them": true, "see": true, "other": true, "than": true, "then": true,
	"now": true, "look": true, "only": true, "come": true, "its": true,
	"over": true, "think": true, "also": true, "back": true, "after": true,
	"use": true, "two": true, "how": true, "our": true, "work": true,
	"first": true, "well": true, "way": true, "even": true, "new": true,
	"want": true, "because": true, "any": true, "these": true, "give": true,
	"day": true, "most": true, "us": true, "is": true, "are": true,
	"was": true, "were": true, "been": true, "has": true, "had": true,
	"did": true, "am": true, "does": true,
}
