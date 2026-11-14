// SPDX-License-Identifier: MIT

package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// countWrites replaces the write seams with counters for the test's duration.
func countWrites(t *testing.T) *int {
	t.Helper()
	var total int
	origCanon, origAppend := writeCanonical, appendJournal
	writeCanonical = func(path string, data []byte, perm os.FileMode) error {
		total += len(data)
		return origCanon(path, data, perm)
	}
	appendJournal = func(path string, data []byte) error {
		total += len(data)
		return origAppend(path, data)
	}
	t.Cleanup(func() { writeCanonical, appendJournal = origCanon, origAppend })
	return &total
}

func addTurn(s *Session, i int) {
	s.Messages = append(s.Messages,
		llm.NewChatMessage("user", "question "+strings.Repeat("q", 200)),
		llm.NewChatMessage("assistant", "answer "+strings.Repeat("a", 200)),
	)
}

func TestSaveBytesPerTurnDoNotGrowWithConversation(t *testing.T) {
	setTestHome(t)
	origFloor := minCompactBytes
	minCompactBytes = 1 << 30 // keep this test on the append path
	t.Cleanup(func() { minCompactBytes = origFloor })
	total := countWrites(t)

	s, _ := New("m", "m:1")
	var perTurn []int
	for i := 0; i < 200; i++ {
		addTurn(s, i)
		before := *total
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		perTurn = append(perTurn, *total-before)
	}
	// Turn 1 writes the snapshot. After that every turn costs about one turn of text.
	early, late := perTurn[5], perTurn[199]
	if late > early*2 {
		t.Fatalf("bytes written at turn 200 = %d, at turn 6 = %d; cost grows with the conversation", late, early)
	}
	if late > 1500 {
		t.Fatalf("a turn of ~500 bytes wrote %d bytes", late)
	}

	got, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 400 {
		t.Fatalf("loaded %d messages, want 400", len(got.Messages))
	}
}

func TestSaveCompactsJournalAndAmortizes(t *testing.T) {
	home := setTestHome(t)
	origFloor := minCompactBytes
	minCompactBytes = 8 * 1024
	t.Cleanup(func() { minCompactBytes = origFloor })
	total := countWrites(t)

	s, _ := New("m", "m:1")
	const turns = 300
	for i := 0; i < turns; i++ {
		addTurn(s, i)
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	// Rewriting the whole file each turn would write the sum of 1..turns turns of text.
	oneTurn := 560
	fullRewrite := oneTurn * 2 * turns * (turns + 1) / 2
	if *total*10 > fullRewrite {
		t.Fatalf("wrote %d bytes over %d turns, full rewrites would write about %d", *total, turns, fullRewrite)
	}
	got, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != turns*2 {
		t.Fatalf("loaded %d messages, want %d", len(got.Messages), turns*2)
	}
	if _, err := os.Stat(filepath.Join(home, ".baryo", "sessions", s.ID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadIgnoresTornJournalLine(t *testing.T) {
	home := setTestHome(t)
	s, _ := New("m", "m:1")
	addTurn(s, 0)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	addTurn(s, 1)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	jp := journalPath(filepath.Join(home, ".baryo", "sessions"), s.ID)
	data, err := os.ReadFile(jp)
	if err != nil {
		t.Fatal(err)
	}
	// Kill mid-write: cut the file in the middle of the last record.
	if err := os.WriteFile(jp, data[:len(data)-100], 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot holds turn 1; the journal held turn 2's two records, the second now torn.
	if len(got.Messages) != 3 {
		t.Fatalf("loaded %d messages, want 3 (history up to the last full line)", len(got.Messages))
	}

	// The next save after a resume must not land behind the torn line.
	got.Messages = append(got.Messages, llm.NewChatMessage("user", "after the crash"))
	if err := got.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) != 4 || *again.Messages[3].Content != "after the crash" {
		t.Fatalf("after resume got %d messages", len(again.Messages))
	}
}

func TestLoadSkipsStaleJournal(t *testing.T) {
	home := setTestHome(t)
	dir := filepath.Join(home, ".baryo", "sessions")
	s, _ := New("m", "m:1")
	addTurn(s, 0)
	_ = s.Save()
	addTurn(s, 1)
	_ = s.Save()
	stale, err := os.ReadFile(journalPath(dir, s.ID))
	if err != nil {
		t.Fatal(err)
	}

	// An earlier message changes in place, so the next save writes a new snapshot.
	text := "rewritten"
	s.Messages[0].Content = &text
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// A crash between writing the snapshot and removing the journal leaves the old one.
	// Its records are of the old generation, and one of them sits exactly at the end of
	// the new snapshot, where only the generation check keeps it out.
	oldGen := strings.Split(string(stale), `"gen":"`)[1][:16]
	atEnd := `{"gen":"` + oldGen + `","ts":"2030-01-01T00:00:00Z","seq":4,"msg":{"role":"user","content":"from the old generation"}}` + "\n"
	if err := os.WriteFile(journalPath(dir, s.ID), append(stale, atEnd...), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 4 || *got.Messages[0].Content != "rewritten" {
		t.Fatalf("stale journal changed the session: %d messages", len(got.Messages))
	}
}

func TestLoadReadsPreJournalSessionFile(t *testing.T) {
	home := setTestHome(t)
	dir := filepath.Join(home, ".baryo", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := Session{
		ID: "oldformat", ModelName: "m", ModelTag: "m:1", CWD: "/x", Title: "old one",
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
		Messages: []llm.ChatMessage{llm.NewChatMessage("user", "hello"), llm.NewChatMessage("assistant", "hi")},
	}
	data, _ := json.MarshalIndent(old, "", "  ")
	if strings.Contains(string(data), "journal_gen") {
		t.Fatal("old-format fixture should not carry a generation")
	}
	if err := os.WriteFile(filepath.Join(dir, "oldformat.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load("oldformat")
	if err != nil || len(got.Messages) != 2 || got.Title != "old one" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	sums, err := List()
	if err != nil || len(sums) != 1 || sums[0].Messages != 2 {
		t.Fatalf("List = %+v, %v", sums, err)
	}

	// Resuming it and saving migrates it: new messages go through the journal.
	got.Messages = append(got.Messages, llm.NewChatMessage("user", "more"))
	if err := got.Save(); err != nil {
		t.Fatal(err)
	}
	got.Messages = append(got.Messages, llm.NewChatMessage("assistant", "ok"))
	if err := got.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journalPath(dir, "oldformat")); err != nil {
		t.Fatalf("expected a journal after migration: %v", err)
	}
	again, err := Load("oldformat")
	if err != nil || len(again.Messages) != 4 {
		t.Fatalf("after migration Load = %v messages, %v", len(again.Messages), err)
	}
}

func TestSaveIntervalHoldsWritesUntilFlush(t *testing.T) {
	setTestHome(t)
	SetSaveInterval(time.Hour)
	t.Cleanup(func() { SetSaveInterval(0) })

	s, _ := New("m", "m:1")
	addTurn(s, 0)
	if err := s.Save(); err != nil { // first save always writes
		t.Fatal(err)
	}
	addTurn(s, 1)
	if err := s.Save(); err != nil { // inside the window: held
		t.Fatal(err)
	}
	got, err := Load(s.ID)
	if err != nil || len(got.Messages) != 2 {
		t.Fatalf("before flush loaded %d messages, %v; want 2", len(got.Messages), err)
	}

	FlushAll()
	got, err = Load(s.ID)
	if err != nil || len(got.Messages) != 4 {
		t.Fatalf("after flush loaded %d messages, %v; want 4", len(got.Messages), err)
	}
}

func TestSaveIntervalTimerWritesHeldSave(t *testing.T) {
	setTestHome(t)
	SetSaveInterval(50 * time.Millisecond)
	t.Cleanup(func() { SetSaveInterval(0) })

	s, _ := New("m", "m:1")
	addTurn(s, 0)
	_ = s.Save()
	addTurn(s, 1)
	_ = s.Save()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got, err := Load(s.ID); err == nil && len(got.Messages) == 4 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("held save was not written when the interval ended")
}

func TestJournalCarriesTitleAndSearchSeesJournaledMessages(t *testing.T) {
	setTestHome(t)
	s, _ := New("m", "m:1")
	s.Messages = append(s.Messages, llm.NewChatMessage("user", "first"))
	_ = s.Save()
	s.Messages = append(s.Messages, llm.NewChatMessage("assistant", "the needle is here"))
	s.Tags = []string{"infra"}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	res, err := Search("needle")
	if err != nil || len(res) != 1 || res[0].Messages != 2 {
		t.Fatalf("Search = %+v, %v", res, err)
	}
	got, _ := Load(s.ID)
	if len(got.Tags) != 1 || got.Tags[0] != "infra" {
		t.Fatalf("tags = %v", got.Tags)
	}
}

func TestCompactFoldsJournalAndCleanOldRemovesIt(t *testing.T) {
	home := setTestHome(t)
	dir := filepath.Join(home, ".baryo", "sessions")
	s, _ := New("m", "m:1")
	addTurn(s, 0)
	_ = s.Save()
	addTurn(s, 1)
	_ = s.Save()

	if err := Compact(s.ID); err != nil {
		t.Fatal(err)
	}
	var snap Session
	data, _ := os.ReadFile(filepath.Join(dir, s.ID+".json"))
	if err := json.Unmarshal(data, &snap); err != nil || len(snap.Messages) != 4 {
		t.Fatalf("snapshot has %d messages, %v; want 4", len(snap.Messages), err)
	}

	// Age the session, and check retention removes the snapshot and the journal.
	snap.UpdatedAt = time.Now().Add(-48 * time.Hour)
	data, _ = json.Marshal(snap)
	if err := os.WriteFile(filepath.Join(dir, s.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := CleanOld(1); err != nil || n != 1 {
		t.Fatalf("CleanOld = %d, %v", n, err)
	}
	if _, err := os.Stat(journalPath(dir, s.ID)); !os.IsNotExist(err) {
		t.Fatalf("CleanOld left the journal behind: %v", err)
	}
}

// A second process (baryo export) folds the journal into the snapshot while the first
// is still saving. Nothing the first process saves afterwards may be lost.
func TestCompactBesideLiveSessionLosesNothing(t *testing.T) {
	setTestHome(t)
	s, _ := New("m", "m:1")
	add := func(n int) {
		for len(s.Messages) < n {
			s.Messages = append(s.Messages, llm.NewChatMessage("user", "m"+strings.Repeat("x", len(s.Messages))))
		}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	add(4)
	add(6)
	if err := Compact(s.ID); err != nil {
		t.Fatal(err)
	}
	add(8)
	got, err := Load(s.ID)
	if err != nil || len(got.Messages) != 8 {
		t.Fatalf("loaded %d messages, %v; want 8", len(got.Messages), err)
	}
}

func writeJournal(t *testing.T, dir string, s *Session, lines ...string) {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(`{"gen":"` + s.Gen + `","ts":"2031-01-01T00:00:00Z",` + l + "}\n")
	}
	if err := os.WriteFile(journalPath(dir, s.ID), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func msgLine(seq int, text string) string {
	return `"seq":` + strconv.Itoa(seq) + `,"msg":{"role":"user","content":"` + text + `"}`
}

func TestReplayStopsAtSequenceGap(t *testing.T) {
	home := setTestHome(t)
	dir := filepath.Join(home, ".baryo", "sessions")
	s, _ := New("m", "m:1")
	addTurn(s, 0)
	_ = s.Save()
	// seq 2 fits, seq 4 leaves a gap, seq 3 after the gap must not be placed.
	writeJournal(t, dir, s, msgLine(2, "two"), msgLine(4, "four"), msgLine(3, "three"))
	got, err := Load(s.ID)
	if err != nil || len(got.Messages) != 3 {
		t.Fatalf("loaded %d messages, %v; want 3", len(got.Messages), err)
	}
}

func TestReplaySkipsRecordsTheSnapshotHolds(t *testing.T) {
	home := setTestHome(t)
	dir := filepath.Join(home, ".baryo", "sessions")
	s, _ := New("m", "m:1")
	addTurn(s, 0)
	_ = s.Save()
	writeJournal(t, dir, s, msgLine(0, "duplicate"), msgLine(2, "two"))
	got, err := Load(s.ID)
	if err != nil || len(got.Messages) != 3 {
		t.Fatalf("loaded %d messages, %v; want 3", len(got.Messages), err)
	}
	if strings.Contains(*got.Messages[0].Content, "duplicate") {
		t.Fatal("a journal record replaced a message the snapshot already held")
	}
}

func TestJournalTimestampSetsUpdatedAt(t *testing.T) {
	setTestHome(t)
	s, _ := New("m", "m:1")
	addTurn(s, 0)
	_ = s.Save()
	future := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	nowFunc = func() time.Time { return future }
	t.Cleanup(func() { nowFunc = time.Now })
	addTurn(s, 1)
	_ = s.Save()
	got, err := Load(s.ID)
	if err != nil || !got.UpdatedAt.Equal(future) {
		t.Fatalf("UpdatedAt = %v, %v; want the journal timestamp %v", got.UpdatedAt, err, future)
	}
}

// A failed append may leave a torn line behind. The next save must start over with a
// snapshot, not append after it, or the failed turn's messages are lost for good.
func TestFailedAppendForcesSnapshotOnNextSave(t *testing.T) {
	setTestHome(t)
	origFloor := minCompactBytes
	minCompactBytes = 1 << 30
	t.Cleanup(func() { minCompactBytes = origFloor })
	s, _ := New("m", "m:1")
	addTurn(s, 0)
	_ = s.Save()

	orig := appendJournal
	appendJournal = func(string, []byte) error { return errors.New("disk full") }
	addTurn(s, 1)
	if err := s.Save(); err == nil {
		t.Fatal("expected the failed append to be reported")
	}
	appendJournal = orig

	addTurn(s, 2)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(s.ID)
	if err != nil || len(got.Messages) != 6 {
		t.Fatalf("loaded %d messages, %v; want 6", len(got.Messages), err)
	}
}

// The journal is folded into the snapshot once it is as big as the snapshot, and not
// before the floor.
func TestJournalCompactionTriggerAndFloor(t *testing.T) {
	home := setTestHome(t)
	dir := filepath.Join(home, ".baryo", "sessions")
	snapshots := 0
	orig := writeCanonical
	writeCanonical = func(p string, d []byte, m os.FileMode) error { snapshots++; return orig(p, d, m) }
	t.Cleanup(func() { writeCanonical = orig })

	// Default floor: 40 turns is about 22 KB of journal, under 64 KiB, so one snapshot.
	s, _ := New("m", "m:1")
	for i := 0; i < 40; i++ {
		addTurn(s, i)
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	if snapshots != 1 {
		t.Fatalf("wrote %d snapshots under the floor, want 1", snapshots)
	}

	// Small floor: the journal must stay near the snapshot size and snapshots must recur.
	if minCompactBytes != 64*1024 {
		t.Fatalf("default compaction floor = %d, want 65536", minCompactBytes)
	}
	minCompactBytes = 4 * 1024
	t.Cleanup(func() { minCompactBytes = 64 * 1024 })
	s2, _ := New("m", "m:1")
	for i := 0; i < 100; i++ {
		addTurn(s2, i)
		if err := s2.Save(); err != nil {
			t.Fatal(err)
		}
	}
	if snapshots < 4 {
		t.Fatalf("only %d snapshots written, the journal was never folded in", snapshots)
	}
	snap, _ := os.Stat(filepath.Join(dir, s2.ID+".json"))
	jr, err := os.Stat(journalPath(dir, s2.ID))
	if err == nil && jr.Size() > snap.Size()+2000 {
		t.Fatalf("journal is %d bytes beside a %d byte snapshot", jr.Size(), snap.Size())
	}
}
