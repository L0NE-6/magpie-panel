package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/provider"
)

// hieuthahn (#7749): claude-mem keeps its data in ~/.claude-mem, with a
// settings.json of its own whose keys are all CLAUDE_MEM_*; that file by
// the name made the folder a "Claude Code · mem" row, and the Library
// ticked every skill on it (421 links written into its skills folder). A
// settings.json counts as Claude Code's only when it holds a key Claude
// Code writes; the sign-in file and projects/ still count on their own,
// and the isolated Claude Desktop's Code tab folder, which magpie makes
// with an empty settings.json (#6459), counts by construction.
func TestClaudeDirAnotherToolsSettingsIsNoClaude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	mem := filepath.Join(home, ".claude-mem")
	writeFile(t, filepath.Join(mem, "settings.json"), `{"CLAUDE_MEM_MODEL":"gpt","CLAUDE_MEM_PROVIDER":"openai"}`)
	writeFile(t, filepath.Join(mem, "claude-mem.db"), "")
	for _, d := range []string{"chroma", "logs", "skills"} {
		if err := os.MkdirAll(filepath.Join(mem, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a folder of Claude Code's by each of the other signs
	writeFile(t, filepath.Join(home, ".claude-work", "settings.json"), `{"theme":"dark"}`)
	writeFile(t, filepath.Join(home, ".claude-creds", ".credentials.json"), `{}`)
	writeFile(t, filepath.Join(home, ".claude-proj", "projects", "p", "s.jsonl"), `{}`)
	// settings.json by the name, but nothing of Claude Code's in it
	writeFile(t, filepath.Join(home, ".claude-empty", "settings.json"), `{}`)
	writeFile(t, filepath.Join(home, ".claude-broken", "settings.json"), `{`)
	writeFile(t, filepath.Join(home, ".claude-list", "settings.json"), `["theme"]`)
	// magpie's own: the isolated Claude Desktop's Code tab folder starts
	// as {} (#6459) and is a row before anything is written into it
	writeFile(t, filepath.Join(desktopIsolatedClaudeDir(home), "settings.json"), "{}\n")

	got := map[string]*Agent{}
	for _, a := range claudeDirAgents(home) {
		got[a.ID] = a
	}
	want := []string{"claude#work", "claude#creds", "claude#proj", "claude#magpie-desktop"}
	for _, id := range want {
		a := got[id]
		if a == nil || !a.Detected() {
			t.Errorf("%s is no row, or not detected: rows %v", id, got)
		}
	}
	for _, id := range []string{"claude#mem", "claude#empty", "claude#broken", "claude#list"} {
		if got[id] != nil {
			t.Errorf("%s is a row: rows %v", id, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	if claudeDirIsClaude(mem) {
		t.Error("claude-mem's folder passes claudeDirIsClaude")
	}
}

// BieJian21872 on X: a second Claude Code (CLAUDE_CONFIG_DIR=~/.claude-work)
// was never found; only ~/.claude was. Each extra folder holding Claude
// Code's files is a row of its own, connected in its own settings.json.
func TestClaudeCodeInMoreThanOneFolder(t *testing.T) {
	// the default folder's settings.json holds a Claude Code key: an
	// object without one is no longer a folder's whole claim (#7749)
	home, _ := claudeDottedHome(t, provider.Provider{ID: "relay", Name: "Relay", Anthropic: "https://example.test", Key: "k",
		Models: []string{"claude-sonnet-4.5", "glm-4.6"}}, `{"theme":"light"}`)
	work := filepath.Join(home, ".claude-work")
	writeFile(t, filepath.Join(work, "settings.json"), `{"theme": "dark"}`)
	writeFile(t, filepath.Join(home, ".claude2", "projects", "p", "s.jsonl"), `{}`)
	// not Claude Code's: another tool's folder, a file, a backup, the launch file's name
	writeFile(t, filepath.Join(home, ".claude-code-router", "config.json"), `{}`)
	writeFile(t, filepath.Join(home, ".claude.json"), `{}`)
	writeFile(t, filepath.Join(home, ".claude.json.backup"), `{}`)
	writeFile(t, filepath.Join(home, ".claude-launch", "settings.json"), `{}`)
	t.Setenv("PATH", t.TempDir())

	got := map[string]*Agent{}
	for _, a := range claudeDirAgents(home) {
		got[a.ID] = a
	}
	if len(got) != 2 || got["claude#work"] == nil || got["claude#2"] == nil {
		t.Fatalf("rows %v, want claude#work and claude#2 only", got)
	}
	// listed on the Agents page with the others
	listed := false
	for _, b := range onMachine(home, filepath.Join(home, ".config")) {
		listed = listed || b.ID == "claude#work"
	}
	if !listed {
		t.Fatal("claude#work isn't among the agents magpie lists")
	}
	a := got["claude#work"]
	if a.Dir != work || !a.Detected() || a.Name != "Claude Code · work" {
		t.Fatalf("row %q at %s detected %v", a.Name, a.Dir, a.Detected())
	}
	if err := a.Field("model").Set("relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, "settings.json")
	if v, _ := edit.GetJSON(path, "env.ANTHROPIC_BASE_URL"); v != gateway.URL() {
		b, _ := os.ReadFile(path)
		t.Fatalf("%s doesn't point at magpie:\n%s", path, b)
	}
	if v, _ := edit.GetJSON(path, "theme"); v != "dark" {
		t.Fatalf("the user's theme is %q", v)
	}
	// the default row is untouched
	if v, _ := edit.GetJSON(filepath.Join(home, ".claude", "settings.json"), "env.ANTHROPIC_BASE_URL"); v != "" {
		t.Fatalf("~/.claude was written: %q", v)
	}
	if !claude(home).Detected() {
		t.Fatal("the default row is gone")
	}

	// with CLAUDE_CONFIG_DIR at one of them, the others (~/.claude too) are rows
	t.Setenv("CLAUDE_CONFIG_DIR", work)
	ids := map[string]bool{}
	for _, a := range claudeDirAgents(home) {
		ids[a.ID] = true
	}
	if ids["claude#work"] || !ids["claude#default"] || !ids["claude#2"] {
		t.Fatalf("rows with CLAUDE_CONFIG_DIR at .claude-work: %v", ids)
	}
}
