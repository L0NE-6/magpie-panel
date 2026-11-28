package sessions

import (
	"strings"
	"testing"
)

// PowerShell's parser takes four Unicode quotation characters as a single
// quote besides ASCII ' (#8048): a cwd holding one, and wrapped in '...' as
// the terminal command wraps it, ends the quoted string there and has what
// follows read as code.
func TestPowerShellQuoteDoublesEveryQuoteChar(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"abc", `'abc'`},
		{"a'b", `'a''b'`},
		{"a\u2018b", "'a\u2018\u2018b'"},
		{"a\u2019b", "'a\u2019\u2019b'"},
		{"a\u201ab", "'a\u201a\u201ab'"},
		{"a\u201bb", "'a\u201b\u201bb'"},
		{"C:\\work\\it\u2019s mine", "'C:\\work\\it\u2019\u2019s mine'"},
		{"", `''`},
	} {
		if got := powerShellQuote(c.in); got != c.want {
			t.Errorf("powerShellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The quoted form is one string: nothing in it but the outer '...' and pairs
// inside, so no quote of any of the five kinds stands alone.
func TestPowerShellQuoteLeavesNoQuoteUnpaired(t *testing.T) {
	for _, in := range []string{"it's", "it\u2019s", "a\u2018b\u2019c\u201ad\u201be", "'\u2019'\u2018"} {
		out := powerShellQuote(in)
		if !strings.HasPrefix(out, "'") || !strings.HasSuffix(out, "'") || len(out) < 2 {
			t.Fatalf("%q -> %q: not one quoted argument", in, out)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(out, "'"), "'")
		for _, q := range []string{"'", "\u2018", "\u2019", "\u201a", "\u201b"} {
			// every occurrence inside must be part of a pair
			n := strings.Count(body, q)
			if n%2 != 0 {
				t.Fatalf("%q -> %q: %q appears %d time(s) inside, an unpaired one ends the string", in, out, q, n)
			}
		}
	}
}
