package usage

import "testing"

// SharedDayLimit is SharedDays unless MAGPIE_SHARED_USAGE_DAYS says otherwise,
// and 0 there keeps every day rather than pruning (#8034).
func TestSharedDayLimit(t *testing.T) {
	for _, c := range []struct {
		env  string
		want int
	}{
		{"", SharedDays},
		{"0", 0},
		{"30", 30},
		{"  7  ", 7},
		{"nonsense", SharedDays},
		{"-5", SharedDays},
	} {
		t.Setenv("MAGPIE_SHARED_USAGE_DAYS", c.env)
		if got := SharedDayLimit(); got != c.want {
			t.Fatalf("MAGPIE_SHARED_USAGE_DAYS=%q: got %d, want %d", c.env, got, c.want)
		}
	}
}
