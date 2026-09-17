// SPDX-License-Identifier: MIT

package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/fsutil"
	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// Session represents a saved conversation.
type Session struct {
	ID        string            `json:"id"`
	ModelName string            `json:"model_name"`
	ModelTag  string            `json:"model_tag"`
	Messages  []llm.ChatMessage `json:"messages"`
	CWD       string            `json:"cwd"`
	Title     string            `json:"title,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// sessionsDir returns ~/.baryo/sessions/, creating it if needed.
func sessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".baryo", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// New creates a new session with a random ID and the current working directory.
func New(modelName, modelTag string) (*Session, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	now := time.Now()
	return &Session{
		ID:        id,
		ModelName: modelName,
		ModelTag:  modelTag,
		CWD:       cwd,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Save writes the session to disk as JSON.
// Generates a title from the first user message if Title is empty.
func (s *Session) Save() error {
	dir, err := sessionsDir()
	if err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	if s.Title == "" && len(s.Messages) > 0 {
		s.GenerateTitle()
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, s.ID+".json"), data, 0o600)
}

// archivePath returns the session's archive file path within dir.
func archivePath(dir, id string) string {
	return filepath.Join(dir, id+".archive.jsonl")
}

// Dir returns the directory holding sessions, creating it if needed. Exported for
// anything that has to hand the location to another process, such as a plugin exporter.
func Dir() (string, error) { return sessionsDir() }

// FilePath returns the session file for an id.
func FilePath(id string) (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}

// ArchivePath returns the archive file for a session. The file may not exist: a session
// that never compacted has nothing archived.
func ArchivePath(id string) (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	return archivePath(dir, id), nil
}

// TracePath returns the trajectory trace file for a session. It sits beside the
// session so retention cleanup covers it.
func TracePath(id string) (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".trace.jsonl"), nil
}

// tracePath is the in-directory form used by cleanup.
func tracePath(dir, id string) string {
	return filepath.Join(dir, id+".trace.jsonl")
}

// maxArchiveLine caps one archive line. A tool result can be large, and the default
// scanner limit of 64KB would silently stop reading at the first one that exceeds it.
const maxArchiveLine = 4 * 1024 * 1024

// ArchivedMessage is one archived message together with the metadata the archive adds:
// when it was archived, and where it sits in the session's sequence.
//
// llm.ChatMessage carries neither, and anything reading history back needs both. An
// exporter has to answer "when did this happen" — ctx's history format, for one,
// requires a timestamp per event — and ordering cannot be recovered from file position
// alone once a corrupt line is skipped.
type ArchivedMessage struct {
	At  time.Time       // when the message was archived, not when it was sent
	Seq int             // position in this session's archive, from 0
	Msg llm.ChatMessage // the message itself
}

// archiveRecord is the on-disk form of ArchivedMessage: one JSON object per line.
type archiveRecord struct {
	TS  time.Time       `json:"ts"`
	Seq int             `json:"seq"`
	Msg llm.ChatMessage `json:"msg"`
}

// Archive appends messages to the session's append-only archive file
// (<id>.archive.jsonl, one JSON record per line). Compaction calls this before
// discarding older messages so the full history survives on disk.
//
// Each record is wrapped in an envelope carrying a timestamp and a sequence number.
// Archives written before the envelope existed hold bare llm.ChatMessage objects, and
// the readers below accept both shapes — a legacy line reports a zero time, which is
// honest, where a guessed one would not be.
func (s *Session) Archive(messages []llm.ChatMessage) error {
	if len(messages) == 0 {
		return nil
	}
	dir, err := sessionsDir()
	if err != nil {
		return err
	}
	seq, err := archiveCount(dir, s.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var buf bytes.Buffer
	for i, msg := range messages {
		data, err := json.Marshal(archiveRecord{TS: now, Seq: seq + i, Msg: msg})
		if err != nil {
			return err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	f, err := os.OpenFile(archivePath(dir, s.ID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// archiveCount returns how many records the archive already holds, which is the next
// sequence number.
//
// Counted from the file rather than held on the Session, because a session can be
// resumed in a new process and a sequence that silently restarts at zero is worse than
// no sequence at all. Legacy lines have no stored seq and are still counted, so numbering
// stays monotonic across the boundary.
func archiveCount(dir, id string) (int, error) {
	f, err := os.Open(archivePath(dir, id))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	n := 0
	sc := newArchiveScanner(f)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) > 0 {
			n++
		}
	}
	return n, sc.Err()
}

// newArchiveScanner returns a scanner sized for archive lines.
func newArchiveScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxArchiveLine)
	return sc
}

// decodeArchiveLine reads either shape of archive line: the current envelope, or a bare
// llm.ChatMessage from before the envelope existed.
//
// The two are told apart by where the role lands. An envelope decoded as a ChatMessage
// has no Role, and a bare message decoded as an envelope has no Msg.Role, because
// encoding/json ignores fields it was not given. Neither shape can be mistaken for the
// other, so no version marker is needed in the file.
func decodeArchiveLine(line []byte) (ArchivedMessage, bool) {
	var rec archiveRecord
	if err := json.Unmarshal(line, &rec); err == nil && rec.Msg.Role != "" {
		return ArchivedMessage{At: rec.TS, Seq: rec.Seq, Msg: rec.Msg}, true
	}
	var msg llm.ChatMessage
	if err := json.Unmarshal(line, &msg); err == nil && msg.Role != "" {
		return ArchivedMessage{Msg: msg}, true
	}
	return ArchivedMessage{}, false
}

// LoadArchiveRecords reads a session's archive with its timestamps and sequence numbers.
// Returns nil with no error if the session has no archive.
func LoadArchiveRecords(id string) ([]ArchivedMessage, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(archivePath(dir, id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var records []ArchivedMessage
	sc := newArchiveScanner(f)
	for sc.Scan() {
		rec, ok := decodeArchiveLine(sc.Bytes())
		if !ok {
			continue // skip corrupt lines rather than losing the rest
		}
		records = append(records, rec)
	}
	return records, sc.Err()
}

// LoadArchive reads all archived (compacted-away) messages for a session.
// Returns nil with no error if the session has no archive.
func LoadArchive(id string) ([]llm.ChatMessage, error) {
	records, err := LoadArchiveRecords(id)
	if err != nil || len(records) == 0 {
		return nil, err
	}
	messages := make([]llm.ChatMessage, 0, len(records))
	for _, rec := range records {
		messages = append(messages, rec.Msg)
	}
	return messages, nil
}

// archiveMatches reports whether any archived message content contains q
// (already lowercased). Missing or unreadable archives simply don't match.
func archiveMatches(dir, id, q string) bool {
	f, err := os.Open(archivePath(dir, id))
	if err != nil {
		return false
	}
	defer f.Close()
	sc := newArchiveScanner(f)
	for sc.Scan() {
		rec, ok := decodeArchiveLine(sc.Bytes())
		if !ok {
			continue
		}
		if rec.Msg.Content != nil && strings.Contains(strings.ToLower(*rec.Msg.Content), q) {
			return true
		}
	}
	return false
}

// GenerateTitle extracts a title from the first user message.
func (s *Session) GenerateTitle() {
	for _, msg := range s.Messages {
		if msg.Role == "user" && msg.Content != nil {
			title := *msg.Content
			// Remove leading slash commands
			if strings.HasPrefix(title, "/") {
				continue
			}
			// Truncate at 60 chars at word boundary
			if len(title) > 60 {
				if idx := strings.LastIndexByte(title[:60], ' '); idx > 20 {
					title = title[:idx] + "..."
				} else {
					title = title[:60] + "..."
				}
			}
			// Take only the first line
			if nl := strings.IndexByte(title, '\n'); nl > 0 {
				title = title[:nl]
			}
			s.Title = title
			return
		}
	}
}

// Load reads a session by ID from disk.
func Load(id string) (*Session, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("corrupt session %q: %w", id, err)
	}
	return &s, nil
}

// Summary is a lightweight view of a session for listing.
type Summary struct {
	ID        string
	ModelName string
	Messages  int
	UpdatedAt time.Time
	CWD       string
	Title     string
	Tags      []string
}

// List returns summaries of all saved sessions, sorted by most recently updated.
func List() ([]Summary, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var summaries []Summary
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		summaries = append(summaries, Summary{
			ID:        s.ID,
			ModelName: s.ModelName,
			Messages:  len(s.Messages),
			UpdatedAt: s.UpdatedAt,
			CWD:       s.CWD,
			Title:     s.Title,
			Tags:      s.Tags,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

// LatestForDir returns the most recently updated session whose CWD matches dir.
func LatestForDir(dir string) (*Session, error) {
	summaries, err := List()
	if err != nil {
		return nil, err
	}
	for _, s := range summaries {
		if s.CWD == dir {
			return Load(s.ID)
		}
	}
	return nil, fmt.Errorf("no session found for %s", dir)
}

// Search scans all sessions for messages matching the query string.
func Search(query string) ([]Summary, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var results []Summary
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		// Check title, then live messages, then archived (compacted-away) messages.
		matched := s.Title != "" && strings.Contains(strings.ToLower(s.Title), q)
		if !matched {
			for _, msg := range s.Messages {
				if msg.Content != nil && strings.Contains(strings.ToLower(*msg.Content), q) {
					matched = true
					break
				}
			}
		}
		if !matched {
			matched = archiveMatches(dir, s.ID, q)
		}
		if matched {
			results = append(results, Summary{
				ID: s.ID, ModelName: s.ModelName, Messages: len(s.Messages),
				UpdatedAt: s.UpdatedAt, CWD: s.CWD, Title: s.Title, Tags: s.Tags,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})
	return results, nil
}

// CleanOld deletes sessions with UpdatedAt older than the given number of days.
// Returns the number of sessions deleted.
func CleanOld(days int) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	dir, err := sessionsDir()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	deleted := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.UpdatedAt.Before(cutoff) {
			if err := os.Remove(path); err == nil {
				deleted++
				os.Remove(archivePath(dir, s.ID)) // best-effort; may not exist
				os.Remove(tracePath(dir, s.ID))   // best-effort; may not exist
			}
		}
	}
	return deleted, nil
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
