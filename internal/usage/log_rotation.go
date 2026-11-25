package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// usageKeep is the history usage.jsonl keeps, the same 30 days as
// routing/. usage.jsonl is the ledger every usage reader parses or indexes,
// so leaving it unbounded made both disk use and every historical read grow
// for as long as magpie was installed (#7739).
var usageKeep = 30 * 24 * time.Hour

// A small ledger is not worth rewriting, and a quiet install may take years
// to reach this floor. Once it is reached, the 30-day window applies; while
// it isn't, the ledger is bounded by the floor itself.
var usageRotateMin int64 = 32 << 20

// The check is daily rather than per append: a busy gateway appends many
// times a second, while a row only becomes old once.
var usageRotateEvery = 24 * time.Hour
var usageRotateRetry = time.Hour

type usageRotationState struct {
	path string
	last time.Time
	next time.Time
}

var usageRotation usageRotationState

// retireOldUsageLocked replaces the ledger with its rows inside usageKeep.
// The caller holds mu, so an append cannot race the copy. On Unix the
// prepared file is renamed over the ledger atomically; Windows cannot
// replace an open file, so it is moved aside and the prepared file takes
// its place. A failed rotation leaves the ledger untouched and is retried
// later. Errors are logged because Append's contract keeps accounting from
// breaking a call.
func retireOldUsageLocked(path string) {
	now := Clock()
	if usageKeep <= 0 {
		return
	}
	if err := recoverUsageRotation(path); err != nil {
		usageRotation.path = path
		usageRotation.last = now
		usageRotation.next = now.Add(usageRotateRetry)
		log.Printf("usage: recovering the ledger after rotation: %v", err)
		return
	}
	if usageRotation.path == path && now.Before(usageRotation.next) && !now.Before(usageRotation.last) {
		return
	}

	usageRotation.path = path
	usageRotation.last = now
	info, err := os.Stat(path)
	if err == nil && info.Size() < usageRotateMin {
		usageRotation.next = now.Add(usageRotateEvery)
		return
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		usageRotation.next = now.Add(usageRotateRetry)
		log.Printf("usage: retiring old rows: %v", err)
		return
	}
	err = rotateUsageLocked(path, now.Add(-usageKeep))
	if err != nil {
		usageRotation.next = now.Add(usageRotateRetry)
		log.Printf("usage: retiring old rows: %v", err)
		return
	}
	usageRotation.next = now.Add(usageRotateEvery)
}

// recoverUsageRotation finishes a Windows replacement interrupted after the
// old ledger was moved aside. If the new ledger is already in place, the old
// file is stale; otherwise it is put back before anything appends to a new
// empty file.
func recoverUsageRotation(path string) error {
	old := path + ".rotating"
	if _, err := os.Stat(old); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return os.Rename(old, path)
	} else if err != nil {
		return err
	}
	return os.Remove(old)
}

func rotateUsageLocked(path string, cutoff time.Time) error {
	in, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	inClosed := false
	defer func() {
		if !inClosed {
			_ = in.Close()
		}
	}()

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".usage-rotate-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	out := bufio.NewWriterSize(tmp, 64<<10)
	rd := bufio.NewReaderSize(in, 64<<10)
	removed := false
	for {
		line, readErr := rd.ReadBytes('\n')
		if len(line) > 0 {
			keep := true
			var r Record
			if json.Unmarshal(line, &r) == nil && !r.Time.IsZero() && r.Time.Before(cutoff) {
				keep = false
				removed = true
			}
			if keep {
				if _, err := out.Write(line); err != nil {
					tmp.Close()
					return err
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			tmp.Close()
			return readErr
		}
	}
	if !removed {
		if err := tmp.Close(); err != nil {
			return err
		}
		return nil
	}
	if err := out.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	inClosed = true

	// openLog reopens after rotation. Closing the old handle first also
	// makes Windows' two-step replacement as reliable as its sharing mode
	// allows.
	if heldLog.f != nil {
		_ = heldLog.f.Close()
		heldLog.f = nil
	}

	if runtime.GOOS != "windows" {
		return os.Rename(tmpPath, path)
	}
	old := path + ".rotating"
	_ = os.Remove(old)
	if err := os.Rename(path, old); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(old, path)
		return err
	}
	_ = os.Remove(old)
	return nil
}
