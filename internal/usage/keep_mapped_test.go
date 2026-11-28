package usage

import "testing"

// keepMapped compares kept's start with b's, to tell whether kept is still
// what b holds. The offset is len(b)-len(kept), which is negative when a
// write since has left kept longer than b - the page's own panic then
// (index out of range [-194] here, [-106] on the reporter's box, #7964).
func TestKeepMappedAKeptLongerThanTheFileIsNotOurs(t *testing.T) {
	wasPath, wasKept := sourceDigestsDisk.path, sourceDigestsDisk.kept
	defer func() { sourceDigestsDisk.path, sourceDigestsDisk.kept = wasPath, wasKept }()

	sourceDigestsDisk.path = "a-path"
	sourceDigestsDisk.kept = make(digestList, 200)
	keepMapped("a-path", []byte("short"))

	if len(sourceDigestsDisk.kept) != 200 || sourceDigestsDisk.path != "a-path" {
		t.Fatalf("kept was taken for what this file holds: %d %q", len(sourceDigestsDisk.kept), sourceDigestsDisk.path)
	}
}
