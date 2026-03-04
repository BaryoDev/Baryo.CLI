// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rag

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arnelirobles/baryo-cli/internal/ignore"
	"github.com/arnelirobles/baryo-cli/internal/index"
)

const (
	maxSourceFileSize = 256 * 1024 // 256KB per file
	maxSourceFiles    = 500
	maxSourceChunk    = 800 // chars per chunk (larger than doc's 500)
	overlapLines      = 3
)

// sourceExts are file extensions included in source indexing.
var sourceExts = map[string]bool{
	".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".py": true, ".rs": true, ".java": true, ".rb": true, ".c": true,
	".cpp": true, ".h": true, ".cs": true, ".swift": true, ".kt": true,
	".sh": true, ".yaml": true, ".yml": true, ".toml": true, ".json": true,
	".md": true,
}

// codeExts are prioritized over config/doc extensions when sorting.
var codeExts = map[string]bool{
	".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".py": true, ".rs": true, ".java": true, ".rb": true, ".c": true,
	".cpp": true, ".h": true, ".cs": true, ".swift": true, ".kt": true,
}

// skipDirs mirrors the skip set from index/filter.go.
var sourceSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
	".venv": true, "dist": true, "build": true, "target": true, "bin": true,
	".next": true,
}

// SourceStore indexes project source files for RAG retrieval.
type SourceStore struct {
	root   string
	chunks []Chunk
	idx    *index.Index // optional — provides symbol boundaries
}

// NewSourceStore creates a source store rooted at the given directory.
// idx may be nil; when available, symbol-based chunking is used.
func NewSourceStore(root string, idx *index.Index) *SourceStore {
	return &SourceStore{root: root, idx: idx}
}

// Build walks the project, reads source files, and chunks them.
func (ss *SourceStore) Build(ctx context.Context) error {
	ss.chunks = nil

	files, err := ss.discoverSourceFiles(ctx)
	if err != nil {
		return fmt.Errorf("rag source: discover: %w", err)
	}

	for _, rel := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		absPath := filepath.Join(ss.root, rel)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if isBinary(data) {
			continue
		}

		content := string(data)

		// Try symbol-based chunking if index is available.
		if ss.idx != nil {
			if fsyms := ss.idx.FileSymbolsFor(rel); fsyms != nil && len(fsyms.Symbols) > 0 {
				ss.chunkBySymbols(rel, content, fsyms)
				continue
			}
		}

		// Fallback: line-based chunking.
		ss.chunkByLines(rel, content)
	}

	return nil
}

// Chunks returns all indexed source chunks.
func (ss *SourceStore) Chunks() []Chunk {
	return ss.chunks
}

// discoverSourceFiles walks the root and returns relative paths of source files,
// prioritizing code files over config/doc files and capping at maxSourceFiles.
func (ss *SourceStore) discoverSourceFiles(ctx context.Context) ([]string, error) {
	var codeFiles []string
	var otherFiles []string

	err := filepath.WalkDir(ss.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		name := d.Name()

		if d.IsDir() {
			if sourceSkipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if !sourceExts[ext] {
			return nil
		}

		info, err := d.Info()
		if err != nil || info.Size() > maxSourceFileSize || info.Size() == 0 {
			return nil
		}

		if ignore.IsIgnored(ctx, path) {
			return nil
		}

		rel, err := filepath.Rel(ss.root, path)
		if err != nil {
			return nil
		}

		if codeExts[ext] {
			codeFiles = append(codeFiles, rel)
		} else {
			otherFiles = append(otherFiles, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort each group by path for deterministic output.
	sort.Strings(codeFiles)
	sort.Strings(otherFiles)

	// Prioritize code files, then config/doc files, capped at maxSourceFiles.
	all := append(codeFiles, otherFiles...)
	if len(all) > maxSourceFiles {
		all = all[:maxSourceFiles]
	}
	return all, nil
}

// chunkBySymbols creates one chunk per symbol (function/type), with file/symbol prefix.
func (ss *SourceStore) chunkBySymbols(rel, content string, fsyms *index.FileSymbols) {
	lines := strings.Split(content, "\n")

	for i, sym := range fsyms.Symbols {
		startLine := sym.Line - 1 // 0-indexed
		if startLine < 0 {
			startLine = 0
		}

		// End line: start of next symbol, or end of file.
		endLine := len(lines)
		if i+1 < len(fsyms.Symbols) {
			endLine = fsyms.Symbols[i+1].Line - 1
		}
		if endLine > len(lines) {
			endLine = len(lines)
		}

		chunk := strings.Join(lines[startLine:endLine], "\n")
		chunk = strings.TrimRight(chunk, "\n ")
		if chunk == "" {
			continue
		}

		// Truncate oversized symbol chunks.
		if len(chunk) > maxSourceChunk*2 {
			chunk = chunk[:maxSourceChunk*2] + "\n// ... (truncated)"
		}

		prefix := fmt.Sprintf("// File: %s\n// %s", rel, sym.Signature)
		ss.chunks = append(ss.chunks, Chunk{
			Source: rel,
			Text:   prefix + "\n" + chunk,
			Type:   "source",
		})
	}
}

// chunkByLines splits file content into overlapping chunks of ~maxSourceChunk chars.
func (ss *SourceStore) chunkByLines(rel, content string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return
	}

	var buf strings.Builder
	startLine := 1 // 1-indexed for display

	for i, line := range lines {
		// Would adding this line exceed the chunk size?
		if buf.Len() > 0 && buf.Len()+len(line)+1 > maxSourceChunk {
			endLine := i // exclusive, 0-indexed = display line i
			prefix := fmt.Sprintf("// File: %s (lines %d-%d)", rel, startLine, endLine)
			ss.chunks = append(ss.chunks, Chunk{
				Source: rel,
				Text:   prefix + "\n" + buf.String(),
				Type:   "source",
			})

			// Reset with overlap.
			buf.Reset()
			overlapStart := i - overlapLines
			if overlapStart < 0 {
				overlapStart = 0
			}
			startLine = overlapStart + 1
			for j := overlapStart; j < i; j++ {
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
				buf.WriteString(lines[j])
			}
		}

		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(line)
	}

	// Flush remaining.
	if buf.Len() > 0 {
		endLine := len(lines)
		prefix := fmt.Sprintf("// File: %s (lines %d-%d)", rel, startLine, endLine)
		ss.chunks = append(ss.chunks, Chunk{
			Source: rel,
			Text:   prefix + "\n" + buf.String(),
			Type:   "source",
		})
	}
}
