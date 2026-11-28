package usage

import (
	"testing"
	"time"
)

// The bounds are read from one session's own calls: the longest gap that
// still read the cache, the shortest after cache activity that did not
// (#8029).
func TestCacheTTLOf(t *testing.T) {
	t0 := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	r := func(min int, read, write, write1h int, session string) Record {
		return Record{
			Time: t0.Add(time.Duration(min) * time.Minute), Agent: "codex",
			Provider: "p", ProviderAccount: "a@b", Model: "m", Session: session,
			CacheRead: read, CacheWrite: write, CacheWrite1h: write1h,
		}
	}
	rs := []Record{
		r(0, 0, 1000, 0, "s1"),
		r(4, 900, 100, 0, "s1"),  // 4 min gap, read: lower bound 4m
		r(6, 800, 0, 0, "s1"),    // 2 min gap, read: lower stays 4m
		r(11, 0, 0, 0, "s1"),     // 5 min gap, no read: upper bound 5m
		r(30, 0, 500, 3600, "s2"), // another session: not a gap of one prefix, but a declared 1h write
	}
	got := CacheTTLOf(rs, "p", "a@b", "m")
	if got.Lower != 4*time.Minute {
		t.Fatalf("lower %v, want 4m", got.Lower)
	}
	if got.Upper != 5*time.Minute {
		t.Fatalf("upper %v, want 5m", got.Upper)
	}
	if got.Reads != 2 || got.Misses != 1 {
		t.Fatalf("reads %d misses %d, want 2 and 1", got.Reads, got.Misses)
	}
	if got.Declared1h != 1 {
		t.Fatalf("declared1h %d, want 1", got.Declared1h)
	}
	// another provider, account or model is not this prefix
	if empty := CacheTTLOf(rs, "q", "a@b", "m"); empty != (CacheTTLBounds{}) {
		t.Fatalf("another provider gave %+v", empty)
	}
	// no records at all must not panic
	if none := CacheTTLOf(nil, "p", "a@b", "m"); none != (CacheTTLBounds{}) {
		t.Fatalf("no records gave %+v", none)
	}
}
