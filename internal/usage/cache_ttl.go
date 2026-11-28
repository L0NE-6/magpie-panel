package usage

import (
	"sort"
	"time"
)

// CacheTTLBounds is what a history says of how long one provider, account
// and model keeps a prompt-cache prefix (#8029), rather than what a
// vendor's page claims: Lower is the longest gap between two calls of one
// session that still read the cache, so the prefix lived at least that
// long; Upper is the shortest gap after cache activity that read none, so
// it had gone by then.
//
// Upper is a naive reading: a call that read no cache may also have missed
// because it failed, because it was a probe or a compaction, or because
// the prompt itself shrank — none of which a usage record tells apart
// today. It is therefore reported with Misses beside it rather than as a
// settled TTL.
type CacheTTLBounds struct {
	Lower  time.Duration
	Upper  time.Duration
	Reads  int // gaps that still read the cache
	Misses int // gaps that did not
	// Declared1h counts the calls whose write was asked to be kept an
	// hour (CacheWrite1h), which is the vendor's own statement of a TTL
	// rather than an observation.
	Declared1h int
}

// CacheTTLOf reads rs, the records of one provider, account and model, in
// any order, and gives what they say of that prefix's life. Records of
// another provider, account or model are left out, as are records with no
// time: a bound from those would be someone else's.
func CacheTTLOf(rs []Record, provider, account, model string) CacheTTLBounds {
	kept := make([]Record, 0, len(rs))
	for _, r := range rs {
		if r.Time.IsZero() || r.Provider != provider || r.ProviderAccount != account || r.Model != model {
			continue
		}
		kept = append(kept, r)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Time.Before(kept[j].Time) })

	var b CacheTTLBounds
	for i := 1; i < len(kept); i++ {
		prev, cur := kept[i-1], kept[i]
		if prev.CacheWrite1h > 0 {
			b.Declared1h++
		}
		// only calls of one session speak of one prefix: another
		// session's first call starts a prefix of its own
		if prev.Agent != cur.Agent || prev.Session != cur.Session {
			continue
		}
		gap := cur.Time.Sub(prev.Time)
		if gap <= 0 {
			continue
		}
		if cur.CacheRead > 0 {
			b.Reads++
			if gap > b.Lower {
				b.Lower = gap
			}
			continue
		}
		if prev.CacheRead == 0 && prev.CacheWrite == 0 {
			continue // nothing was kept before this call to have gone
		}
		b.Misses++
		if b.Upper == 0 || gap < b.Upper {
			b.Upper = gap
		}
	}
	if len(kept) == 0 {
		return CacheTTLBounds{}
	}
	if kept[len(kept)-1].CacheWrite1h > 0 {
		b.Declared1h++
	}
	return b
}
