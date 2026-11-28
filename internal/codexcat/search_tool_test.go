package codexcat

import "testing"

// An entry carrying the vendor's own says nothing of Codex's code-mode tool
// search, which is the client's: silent on it, the entry gets it, or every
// tool description goes whole in the first request - 181,790 tokens in the
// reporter's session against 25,451 with this on (#7904).
func TestSearchToolUnlessSaid(t *testing.T) {
	silent := map[string]any{"tool_mode": "code_mode_only"}
	searchToolUnlessSaid(silent)
	if silent["supports_search_tool"] != true {
		t.Fatalf("a silent entry was left without tool search: %v", silent)
	}

	said := map[string]any{"supports_search_tool": false}
	searchToolUnlessSaid(said)
	if said["supports_search_tool"] != false {
		t.Fatalf("a vendor that answered false was overridden: %v", said)
	}
}
