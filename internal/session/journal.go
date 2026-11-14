// SPDX-License-Identifier: MIT

package session

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/fsutil"
	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// The live session is saved as two files. <id>.json is the canonical snapshot and keeps
// the shape it always had, because exporters read it directly. <id>.journal.jsonl holds
// what happened since that snapshot, one JSON record per line, appended and never
// rewritten. A turn costs one small append instead of a rewrite of the whole
// conversation, which is what matters on SD and eMMC storage.
//
// The journal is folded back into the snapshot (a "compaction", not to be confused with
// conversation compaction) when it has grown as large as the snapshot, so the full
// rewrites amortize to a constant number of bytes per turn, and whenever an earlier
// message changed in place, since an append cannot express that.
//
// Crash safety, as in v0.12.1: the snapshot is only ever replaced by an atomic rename. A
// journal append that is cut short leaves a last line with no newline, which Load
// ignores. Each snapshot carries a generation id and every journal record carries the id
// of the snapshot it extends, so a journal left behind by a crash between "write the new
// snapshot" and "remove the old journal" is recognised as stale and skipped.

// minCompactBytes is the smallest journal worth folding into the snapshot.
var minCompactBytes int64 = 64 * 1024

// Seams for the file writes, so tests can count the bytes a turn costs.
var (
	writeCanonical = fsutil.WriteFileAtomic
	appendJournal  = func(path string, data []byte) error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	nowFunc = time.Now
)

var saveInterval atomic.Int64 // nanoseconds; 0 writes on every Save

// SetSaveInterval sets the shortest time between writes of a session to disk. With zero,
// every Save writes its small append at once. With a positive interval a Save inside the
// window is held in memory and written when the window ends, on Flush, or at exit, so a
// hard kill loses at most that window.
func SetSaveInterval(d time.Duration) {
	if d < 0 {
		d = 0
	}
	saveInterval.Store(int64(d))
}

// journalRecord is one line of the journal. A message record has Seq and Msg, where Seq
// is the message's index in the conversation. A metadata record has Title or Tags.
type journalRecord struct {
	Gen   string           `json:"gen"`
	TS    time.Time        `json:"ts"`
	Seq   *int             `json:"seq,omitempty"`
	Msg   *llm.ChatMessage `json:"msg,omitempty"`
	Title *string          `json:"title,omitempty"`
	Tags  *[]string        `json:"tags,omitempty"`
}

func journalPath(dir, id string) string {
	return filepath.Join(dir, id+".journal.jsonl")
}

// saveState is what a Session remembers between saves: the fingerprint of every message
// already covered by the snapshot or the journal, and what is waiting to be written.
type saveState struct {
	mu     sync.Mutex
	dir    string
	id     string
	gen    string // snapshot generation the journal extends; empty forces a snapshot
	hashes []uint64
	title  string
	tags   string

	canonSize   int64
	journalSize int64

	pendingCanon   []byte
	pendingJournal []byte

	lastWrite time.Time
	timer     *time.Timer
}

var (
	liveMu sync.Mutex
	live   = map[*saveState]struct{}{}
)

func (s *Session) state(dir string) *saveState {
	if s.st == nil {
		s.st = &saveState{dir: dir, id: s.ID}
		liveMu.Lock()
		live[s.st] = struct{}{}
		liveMu.Unlock()
	}
	return s.st
}

func hashMessage(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
}

// save plans the write for the session's current state and performs it unless the
// interval says to wait.
func (st *saveState) save(s *Session) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Fingerprinting every message costs CPU, not storage writes. It is what lets an
	// append be trusted: if an earlier message changed, the journal cannot say so.
	enc := make([][]byte, len(s.Messages))
	cur := make([]uint64, len(s.Messages))
	for i, m := range s.Messages {
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		enc[i] = data
		cur[i] = hashMessage(data)
	}

	tagsKey := strings.Join(s.Tags, "\x00")
	appendOK := st.gen != "" && len(cur) >= len(st.hashes)
	if appendOK {
		for i, h := range st.hashes {
			if cur[i] != h {
				appendOK = false
				break
			}
		}
	}

	var lines []byte
	if appendOK {
		now := nowFunc().UTC()
		if s.Title != st.title || tagsKey != st.tags {
			title, tags := s.Title, append([]string{}, s.Tags...)
			data, err := json.Marshal(journalRecord{Gen: st.gen, TS: now, Title: &title, Tags: &tags})
			if err != nil {
				return err
			}
			lines = append(append(lines, data...), '\n')
		}
		for i := len(st.hashes); i < len(cur); i++ {
			seq := i
			var msg llm.ChatMessage
			if err := json.Unmarshal(enc[i], &msg); err != nil {
				return err
			}
			data, err := json.Marshal(journalRecord{Gen: st.gen, TS: now, Seq: &seq, Msg: &msg})
			if err != nil {
				return err
			}
			lines = append(append(lines, data...), '\n')
		}
		if len(lines) == 0 {
			return st.maybeWrite() // nothing new; still release anything held
		}
		if st.journalSize+int64(len(lines)) >= max(minCompactBytes, st.canonSize) {
			appendOK = false
		}
	}

	if appendOK {
		st.pendingJournal = append(st.pendingJournal, lines...)
		st.journalSize += int64(len(lines))
		st.hashes = cur
		st.title, st.tags = s.Title, tagsKey
		return st.maybeWrite()
	}

	gen, err := randomID()
	if err != nil {
		return err
	}
	s.Gen = gen
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	st.pendingCanon = data
	st.pendingJournal = nil
	st.gen = gen
	st.hashes = cur
	st.title, st.tags = s.Title, tagsKey
	st.canonSize = int64(len(data))
	st.journalSize = 0
	return st.maybeWrite()
}

// maybeWrite writes what is pending now, or arms a timer to write it when the save
// interval ends. Called with st.mu held.
func (st *saveState) maybeWrite() error {
	if st.pendingCanon == nil && len(st.pendingJournal) == 0 {
		return nil
	}
	interval := time.Duration(saveInterval.Load())
	if interval > 0 && !st.lastWrite.IsZero() {
		if wait := interval - nowFunc().Sub(st.lastWrite); wait > 0 {
			if st.timer == nil {
				st.timer = time.AfterFunc(wait, func() { _ = st.flush() })
			}
			return nil
		}
	}
	return st.flushLocked()
}

func (st *saveState) flush() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.flushLocked()
}

func (st *saveState) flushLocked() error {
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	canon, journal := st.pendingCanon, st.pendingJournal
	if canon == nil && len(journal) == 0 {
		return nil
	}
	st.pendingCanon, st.pendingJournal = nil, nil
	st.lastWrite = nowFunc()

	fail := func(err error) error {
		// A cut-off append would leave a torn line that the next append lands behind.
		// Dropping the generation makes the next Save write a fresh snapshot, which
		// also discards the journal.
		st.gen = ""
		return err
	}
	if canon != nil {
		if err := writeCanonical(filepath.Join(st.dir, st.id+".json"), canon, 0o600); err != nil {
			return fail(err)
		}
		// Safe to fail: records of the old generation are ignored on load.
		os.Remove(journalPath(st.dir, st.id))
	}
	if len(journal) > 0 {
		if err := appendJournal(journalPath(st.dir, st.id), journal); err != nil {
			return fail(err)
		}
	}
	return nil
}

// Flush writes anything a debounced Save is still holding.
func (s *Session) Flush() error {
	if s.st == nil {
		return nil
	}
	return s.st.flush()
}

// FlushAll flushes every session this process has saved. Call it on exit and on signal.
func FlushAll() {
	liveMu.Lock()
	states := make([]*saveState, 0, len(live))
	for st := range live {
		states = append(states, st)
	}
	liveMu.Unlock()
	for _, st := range states {
		_ = st.flush()
	}
}

// applyJournal replays the journal onto a snapshot loaded from disk. Records of another
// generation, records for messages the snapshot already holds, and a torn last line are
// skipped. A gap in the sequence ends the replay, because nothing after it can be placed.
func applyJournal(dir string, s *Session) {
	if s.Gen == "" {
		return
	}
	data, err := os.ReadFile(journalPath(dir, s.ID))
	if err != nil {
		return
	}
	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			return // no newline: the write was cut short
		}
		line := data[:nl]
		data = data[nl+1:]
		var rec journalRecord
		if err := json.Unmarshal(line, &rec); err != nil || rec.Gen != s.Gen {
			continue
		}
		switch {
		case rec.Msg != nil && rec.Seq != nil:
			switch {
			case *rec.Seq < len(s.Messages):
				continue
			case *rec.Seq > len(s.Messages):
				return
			}
			s.Messages = append(s.Messages, *rec.Msg)
		case rec.Title != nil || rec.Tags != nil:
			if rec.Title != nil {
				s.Title = *rec.Title
			}
			if rec.Tags != nil {
				s.Tags = *rec.Tags
			}
		default:
			continue
		}
		if rec.TS.After(s.UpdatedAt) {
			s.UpdatedAt = rec.TS
		}
	}
}

// readSession reads the snapshot and replays its journal.
func readSession(dir, id string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	applyJournal(dir, &s)
	return &s, nil
}
