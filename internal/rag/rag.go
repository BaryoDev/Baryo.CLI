// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rag

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// RAG orchestrates retrieval from document and session stores.
type RAG struct {
	mu       sync.RWMutex
	docs     *DocumentStore
	sessions *SessionStore
	docIndex *BM25Index
	sesIndex *BM25Index
	ready    bool
}

// New creates a RAG pipeline for the given project root and working directory.
func New(projectRoot, cwd string) *RAG {
	return &RAG{
		docs:     NewDocumentStore(projectRoot),
		sessions: NewSessionStore(cwd),
		docIndex: NewBM25Index(),
		sesIndex: NewBM25Index(),
	}
}

// Build scans knowledge files and past sessions, then indexes them.
func (r *RAG) Build(ctx context.Context) error {
	// Build both stores (order doesn't matter, but keep it simple/sequential
	// since this runs in a background goroutine already).
	if err := r.docs.Build(ctx); err != nil {
		return fmt.Errorf("rag: document build: %w", err)
	}
	if err := r.sessions.Build(ctx); err != nil {
		return fmt.Errorf("rag: session build: %w", err)
	}

	// Index chunks.
	if chunks := r.docs.Chunks(); len(chunks) > 0 {
		r.docIndex.Add(chunks)
	}
	if chunks := r.sessions.Chunks(); len(chunks) > 0 {
		r.sesIndex.Add(chunks)
	}

	r.mu.Lock()
	r.ready = true
	r.mu.Unlock()
	return nil
}

// Ready returns true once the initial build has completed.
func (r *RAG) Ready() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ready
}

// DocCount returns the number of indexed document chunks.
func (r *RAG) DocCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.docs.Chunks())
}

// SessionCount returns the number of indexed session chunks.
func (r *RAG) SessionCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions.Chunks())
}

// NeedsFreshInfo checks whether the query needs up-to-date web info.
func (r *RAG) NeedsFreshInfo(query string) bool {
	return NeedsFreshInfo(query)
}

// Query runs BM25 search across both indexes and returns a formatted context
// block within the given token budget.
func (r *RAG) Query(userMessage string, budgetTokens int) string {
	if budgetTokens <= 0 || !r.Ready() {
		return ""
	}

	// Budget split: 60% documents, 40% sessions.
	docBudget := budgetTokens * 60 / 100
	sesBudget := budgetTokens - docBudget

	docChunks := r.docIndex.Query(userMessage, 10)
	sesChunks := r.sesIndex.Query(userMessage, 10)

	// Format within budget (approximate: 4 chars per token).
	docSection := formatChunks(docChunks, docBudget*4)
	sesSection := formatChunks(sesChunks, sesBudget*4)

	if docSection == "" && sesSection == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("<context>")
	if docSection != "" {
		b.WriteString("\n<documents>\n")
		b.WriteString(docSection)
		b.WriteString("\n</documents>")
	}
	if sesSection != "" {
		b.WriteString("\n<sessions>\n")
		b.WriteString(sesSection)
		b.WriteString("\n</sessions>")
	}
	b.WriteString("\n</context>")
	return b.String()
}

// formatChunks renders scored chunks within a character budget.
func formatChunks(chunks []Chunk, budgetChars int) string {
	if len(chunks) == 0 || budgetChars <= 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range chunks {
		entry := c.Text
		if c.Source != "" {
			entry = fmt.Sprintf("[%s]\n%s", c.Source, c.Text)
		}
		if b.Len()+len(entry)+2 > budgetChars {
			break
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(entry)
	}
	return b.String()
}
