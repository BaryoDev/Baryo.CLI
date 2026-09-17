// SPDX-License-Identifier: MIT

// Package export turns saved sessions into a portable form.
//
// It serves two consumers. The built-in `baryo-jsonl` format is Baryo's own history,
// written as one self-describing JSONL file. Plugin exporters receive the same session list
// and write whatever format they target — ctx's history format being the case this was
// built for.
//
// Keeping a built-in format here is what makes the plugin contract honest: the native
// export exercises the same request an external exporter gets, so the contract cannot
// quietly become "whatever one plugin happens to need".
package export

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
	"github.com/arnelirobles/baryo-cli/internal/plugin"
	"github.com/arnelirobles/baryo-cli/internal/session"
)

// BuiltinID is the format every build can write without a plugin installed.
const BuiltinID = "baryo-jsonl"

// BuiltinSchema versions the native format. An external adapter reading these files should
// refuse a schema it does not know rather than guess at the shape.
//
// On time: no event carries an occurred_at, because nothing in Baryo records when a message
// was sent. An archived event carries archived_at, an upper bound, and every event declares
// time_fidelity so an adapter can map it to its own format's fidelity field rather than
// assuming. ctx's history format, for one, has exactly that notion.
const BuiltinSchema = "baryo-history-jsonl-v1"

// Gather collects the sessions to export.
//
// since filters on a session's last update; a zero time takes everything. onlyID limits the
// export to one session. Sessions are ordered oldest first, because an importer appending
// to an index wants history in the order it happened.
func Gather(since time.Time, onlyID string) (string, []plugin.SessionRef, error) {
	dir, err := session.Dir()
	if err != nil {
		return "", nil, err
	}

	var summaries []session.Summary
	if onlyID != "" {
		s, err := session.Load(onlyID)
		if err != nil {
			return "", nil, err
		}
		summaries = []session.Summary{{ID: s.ID, UpdatedAt: s.UpdatedAt}}
	} else {
		summaries, err = session.List()
		if err != nil {
			return "", nil, err
		}
	}

	var refs []plugin.SessionRef
	for _, sum := range summaries {
		if !since.IsZero() && sum.UpdatedAt.Before(since) {
			continue
		}
		s, err := session.Load(sum.ID)
		if err != nil {
			continue // a session that cannot be read is skipped, not fatal
		}
		ref := plugin.SessionRef{
			ID:        s.ID,
			Title:     s.Title,
			ModelName: s.ModelName,
			ModelTag:  s.ModelTag,
			CWD:       s.CWD,
			CreatedAt: rfc3339(s.CreatedAt),
			UpdatedAt: rfc3339(s.UpdatedAt),
		}
		if p, err := session.FilePath(s.ID); err == nil {
			ref.MessagesPath = p
		}
		// Archive and trace are optional: a session that never compacted has no archive,
		// and one run without --trace-file has no trace. Only existing paths are sent, so
		// an exporter does not have to distinguish "absent" from "unreadable".
		if p, err := session.ArchivePath(s.ID); err == nil && exists(p) {
			ref.ArchivePath = p
		}
		if p, err := session.TracePath(s.ID); err == nil && exists(p) {
			ref.TracePath = p
		}
		refs = append(refs, ref)
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].UpdatedAt < refs[j].UpdatedAt })
	return dir, refs, nil
}

// Builtin writes the native JSONL export and returns what it wrote.
func Builtin(req plugin.ExportRequest) (plugin.ExportResponse, error) {
	if err := os.MkdirAll(req.OutDir, 0o700); err != nil {
		return plugin.ExportResponse{}, fmt.Errorf("creating %s: %w", req.OutDir, err)
	}
	out := filepath.Join(req.OutDir, "baryo-history.jsonl")
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return plugin.ExportResponse{}, err
	}
	defer f.Close()

	resp := plugin.ExportResponse{OK: true, Written: []string{out}}
	enc := json.NewEncoder(f)

	if err := enc.Encode(map[string]any{
		"record_type":    "manifest",
		"schema_version": BuiltinSchema,
		"producer":       "baryo/" + req.BaryoVer,
		"exported_at":    rfc3339(time.Now().UTC()),
	}); err != nil {
		return resp, err
	}

	undated := 0
	for _, ref := range req.Sessions {
		if err := enc.Encode(map[string]any{
			"record_type": "session",
			"session_id":  ref.ID,
			"title":       ref.Title,
			"model_name":  ref.ModelName,
			"model_tag":   ref.ModelTag,
			"cwd":         ref.CWD,
			"started_at":  ref.CreatedAt,
			"ended_at":    ref.UpdatedAt,
		}); err != nil {
			return resp, err
		}
		resp.Sessions++

		n, missing, err := writeEvents(enc, ref)
		if err != nil {
			return resp, err
		}
		resp.Events += n
		undated += missing
	}

	if undated > 0 {
		// Said plainly rather than papered over with a guessed timestamp. An importer can
		// decide what to do with an undated record; it cannot undo an invented one.
		resp.Warnings = append(resp.Warnings, fmt.Sprintf(
			"%d archived messages predate archive timestamps and carry no time at all", undated))
	}
	return resp, nil
}

// writeEvents writes one event per message: first the archived messages, which compaction
// removed from the conversation, then the messages still live in it.
//
// Archive first is the real order, not a convention: compaction appends what it is about to
// discard, so everything in the archive happened before what remains.
func writeEvents(enc *json.Encoder, ref plugin.SessionRef) (count, undated int, err error) {
	idx := 0

	records, err := session.LoadArchiveRecords(ref.ID)
	if err != nil {
		return 0, 0, err
	}
	for _, rec := range records {
		at := ""
		if rec.At.IsZero() {
			undated++
		} else {
			at = rfc3339(rec.At)
		}
		if err := encodeEvent(enc, ref.ID, idx, at, "archived", rec.Msg); err != nil {
			return count, undated, err
		}
		idx++
		count++
	}

	s, err := session.Load(ref.ID)
	if err != nil {
		return count, undated, nil // already counted what we could read
	}
	for _, msg := range s.Messages {
		// Live messages have no time of their own and none is invented for them: the
		// session record carries started_at and ended_at, which bound the whole session,
		// and that is the honest extent of what is known.
		if err := encodeEvent(enc, ref.ID, idx, "", "live", msg); err != nil {
			return count, undated, err
		}
		idx++
		count++
	}
	return count, undated, nil
}

// Time fidelity values. A consumer has to know what a timestamp means before it can use
// one, and this format cannot currently offer a message's own time for any record.
const (
	// fidelityUpperBound: the record carries archived_at, the moment compaction wrote the
	// message away. The message happened at or before it, by an unknown margin.
	fidelityUpperBound = "archived_upper_bound"
	// fidelityUnknown: no time at all. Live messages and archives written before the
	// envelope existed.
	fidelityUnknown = "unknown"
)

// encodeEvent writes one event record.
//
// There is deliberately no occurred_at field. The only time available is when compaction
// archived a message, and llm.ChatMessage has no timestamp of its own, so a field named
// occurred_at would invite an importer to read an upper bound as the moment the message
// happened — the same mistake as inventing a timestamp, one level up. What is known is
// published under archived_at, and every record says what its time is worth in
// time_fidelity.
func encodeEvent(enc *json.Encoder, sessionID string, idx int, archivedAt, origin string, msg llm.ChatMessage) error {
	rec := map[string]any{
		"record_type":   "event",
		"session_id":    sessionID,
		"event_index":   idx,
		"origin":        origin, // archived or live
		"role":          msg.Role,
		"event_type":    eventType(msg),
		"time_fidelity": fidelityUnknown,
		"message":       msg,
	}
	if archivedAt != "" {
		rec["archived_at"] = archivedAt
		rec["time_fidelity"] = fidelityUpperBound
	}
	return enc.Encode(rec)
}

// eventType classifies a message so a consumer does not have to re-derive it. A tool call
// and its result are the part of history a summary loses, which is the whole reason for
// exporting raw messages rather than summaries.
func eventType(msg llm.ChatMessage) string {
	switch {
	case len(msg.ToolCalls) > 0:
		return "tool_call"
	case msg.ToolCallID != "":
		return "tool_result"
	default:
		return "message"
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Describe writes a human summary of a response.
func Describe(w io.Writer, format string, resp plugin.ExportResponse) {
	fmt.Fprintf(w, "Exported %d sessions, %d events as %s\n", resp.Sessions, resp.Events, format)
	for _, p := range resp.Written {
		fmt.Fprintf(w, "  %s\n", p)
	}
	for _, warn := range resp.Warnings {
		fmt.Fprintf(w, "  warning: %s\n", warn)
	}
}
