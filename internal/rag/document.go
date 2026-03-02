// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxFileSize  = 1 << 20 // 1MB
	maxChunkSize = 500     // characters
)

var knowledgeExts = map[string]bool{
	".md":  true,
	".txt": true,
	".rst": true,
}

// DocumentStore indexes knowledge files from global and project directories.
type DocumentStore struct {
	projectRoot string
	chunks      []Chunk
}

// NewDocumentStore creates a document store that will scan both global and
// project-local knowledge directories.
func NewDocumentStore(projectRoot string) *DocumentStore {
	return &DocumentStore{projectRoot: projectRoot}
}

// Build scans knowledge directories and chunks the files.
func (ds *DocumentStore) Build(ctx context.Context) error {
	ds.chunks = nil

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dirs := []string{
		filepath.Join(home, ".baryo", "knowledge"),
	}
	if ds.projectRoot != "" {
		dirs = append(dirs, filepath.Join(ds.projectRoot, ".baryo", "knowledge"))
	}

	for _, dir := range dirs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ds.scanDir(dir)
	}
	return nil
}

// Chunks returns all indexed document chunks.
func (ds *DocumentStore) Chunks() []Chunk {
	return ds.chunks
}

func (ds *DocumentStore) scanDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory doesn't exist — not an error
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !knowledgeExts[ext] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > maxFileSize || info.Size() == 0 {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		// Skip binary-looking files.
		if isBinary(data) {
			continue
		}

		source := filepath.Join(dir, e.Name())
		for _, chunk := range chunkText(string(data)) {
			ds.chunks = append(ds.chunks, Chunk{
				Source: source,
				Text:   chunk,
				Type:   "document",
			})
		}
	}
}

// chunkText splits text on paragraph boundaries (\n\n), merging small
// adjacent paragraphs and splitting large ones to stay near maxChunkSize.
func chunkText(text string) []string {
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var buf strings.Builder

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// If adding this paragraph would exceed the limit, flush the buffer.
		if buf.Len() > 0 && buf.Len()+len(p)+2 > maxChunkSize {
			chunks = append(chunks, buf.String())
			buf.Reset()
		}

		// If a single paragraph is larger than maxChunkSize, split it.
		if len(p) > maxChunkSize {
			if buf.Len() > 0 {
				chunks = append(chunks, buf.String())
				buf.Reset()
			}
			chunks = append(chunks, splitLong(p)...)
			continue
		}

		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(p)
	}

	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}
	return chunks
}

// splitLong breaks a long paragraph into ~maxChunkSize pieces at word boundaries.
func splitLong(text string) []string {
	words := strings.Fields(text)
	var chunks []string
	var buf strings.Builder

	for _, w := range words {
		if buf.Len() > 0 && buf.Len()+len(w)+1 > maxChunkSize {
			chunks = append(chunks, buf.String())
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(w)
	}
	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}
	return chunks
}

// isBinary returns true if the first 512 bytes contain a NUL byte.
func isBinary(data []byte) bool {
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}
