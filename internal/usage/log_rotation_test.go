package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writeUsageRecords(t *testing.T, recs ...Record) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func usageAgents(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, r := range Load(time.Time{}) {
		out[r.Agent]++
	}
	return out
}

func enableUsageRotationForTest(t *testing.T) {
	t.Helper()
	keep, every, retry, min := usageKeep, usageRotateEvery, usageRotateRetry, usageRotateMin
	state := usageRotation
	usageRotateMin = 0
	usageRotation = usageRotationState{}
	t.Cleanup(func() {
		usageKeep, usageRotateEvery, usageRotateRetry, usageRotateMin = keep, every, retry, min
		usageRotation = state
	})
}

// usage.jsonl keeps the same bounded history as routing/: appending after
// the retention window rewrites the ledger without rows older than it.
func TestAppendRetiresUsageOlderThanRetention(t *testing.T) {
	pageHome(t)
	enableUsageRotationForTest(t)
	now := holdClock(t, time.Date(2026, 11, 26, 12, 0, 0, 0, time.Local))
	writeUsageRecords(t,
		Record{Time: now.Add(-usageKeep - time.Hour), Agent: "old", Provider: "relay", Model: "m", Status: 200},
		Record{Time: now.Add(-usageKeep + time.Hour), Agent: "recent", Provider: "relay", Model: "m", Status: 200},
	)
	Append(Record{Agent: "new", Provider: "relay", Model: "m", Status: 200})
	got := usageAgents(t)
	if got["old"] != 0 || got["recent"] != 1 || got["new"] != 1 {
		t.Fatalf("after retention: %v", got)
	}
}

// A snapshot built before rotation must not keep serving the retired rows.
func TestUsageRotationInvalidatesIndex(t *testing.T) {
	pageHome(t)
	enableUsageRotationForTest(t)
	now := holdClock(t, time.Date(2026, 11, 26, 12, 0, 0, 0, time.Local))
	writeUsageRecords(t,
		Record{Time: now.Add(-usageKeep - time.Hour), Agent: "old", Provider: "relay", Model: "m", Status: 200},
		Record{Time: now.Add(-time.Hour), Agent: "recent", Provider: "relay", Model: "m", Status: 200},
	)
	if got := Summarize(All); got.Calls != 2 {
		t.Fatalf("before rotation: %+v", got)
	}

	Append(Record{Agent: "new", Provider: "relay", Model: "m", Status: 200})
	got := Summarize(All)
	if got.Calls != 2 {
		t.Fatalf("after rotation: %+v", got)
	}
}

// Every request already being appended when the ledger rotates is still
// in the new file.
func TestUsageRotationKeepsConcurrentAppends(t *testing.T) {
	pageHome(t)
	enableUsageRotationForTest(t)
	now := holdClock(t, time.Date(2026, 11, 26, 12, 0, 0, 0, time.Local))
	writeUsageRecords(t, Record{Time: now.Add(-usageKeep - time.Hour), Agent: "old", Provider: "relay", Model: "m", Status: 200})

	const n = 48
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Append(Record{Time: now, Agent: fmt.Sprintf("new-%02d", i), Provider: "relay", Model: "m", Status: 200})
		}()
	}
	wg.Wait()

	got := usageAgents(t)
	if got["old"] != 0 {
		t.Fatalf("old row survived: %v", got)
	}
	for i := range n {
		if got[fmt.Sprintf("new-%02d", i)] != 1 {
			t.Fatalf("new-%02d lost: %v", i, got)
		}
	}
}

// Nothing is rewritten while every row is still inside the window.
func TestUsageAppendKeepsLedgerWhenAllRowsAreRecent(t *testing.T) {
	pageHome(t)
	enableUsageRotationForTest(t)
	now := holdClock(t, time.Date(2026, 11, 26, 12, 0, 0, 0, time.Local))
	writeUsageRecords(t, Record{Time: now.Add(-time.Hour), Agent: "recent", Provider: "relay", Model: "m", Status: 200})
	before, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}

	Append(Record{Agent: "new", Provider: "relay", Model: "m", Status: 200})
	after, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("a ledger with no retired rows was replaced")
	}
}

// A tiny ledger is left alone even when it holds an old row: rewriting it
// would cost more than it saves, and the 32 MiB floor is its bound until
// it grows enough for the age window to take over.
func TestUsageRotationWaitsForSizeFloor(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 11, 26, 12, 0, 0, 0, time.Local))
	writeUsageRecords(t, Record{Time: now.Add(-usageKeep - time.Hour), Agent: "old", Provider: "relay", Model: "m", Status: 200})
	Append(Record{Agent: "new", Provider: "relay", Model: "m", Status: 200})
	if got := usageAgents(t); got["old"] != 1 || got["new"] != 1 {
		t.Fatalf("a ledger below the size floor was rewritten: %v", got)
	}
}

// A Windows replacement interrupted between its two renames puts the old
// ledger back before appending, so the kept rows are not stranded beside a
// new empty usage.jsonl.
func TestUsageRotationRecoversInterruptedReplacement(t *testing.T) {
	pageHome(t)
	enableUsageRotationForTest(t)
	now := holdClock(t, time.Date(2026, 11, 26, 12, 0, 0, 0, time.Local))
	writeUsageRecords(t, Record{Time: now.Add(-time.Hour), Agent: "recent", Provider: "relay", Model: "m", Status: 200})
	old := Path() + ".rotating"
	if err := os.Rename(Path(), old); err != nil {
		t.Fatal(err)
	}

	Append(Record{Agent: "new", Provider: "relay", Model: "m", Status: 200})
	if got := usageAgents(t); got["recent"] != 1 || got["new"] != 1 {
		t.Fatalf("recovered ledger: %v", got)
	}
}
