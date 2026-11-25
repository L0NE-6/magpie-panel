package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linkSharedToLibrary(t *testing.T, h string) {
	t.Helper()
	shared := filepath.Join(h, ".agents/skills")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dirLink(skillsDir(), shared); err != nil {
		t.Fatal(err)
	}
}

func setUpAliasTicks(t *testing.T) string {
	t.Helper()
	h := sandbox(t)
	noGoose(t, h)
	src := filepath.Join(h, "my-skills")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		skill(t, filepath.Join(src, name), name, "The "+name)
	}
	ok(t)(InstallSkills(src, []string{"alpha", "beta", "gamma"}, []string{"claude"}))
	ok(t)(SkillAgents("gamma", []string{"claude", "codex"}))
	return h
}

func checkAliasTicks(t *testing.T, h string) {
	t.Helper()
	if !sharedIsLibrary() {
		t.Fatal("the shared folder alias is not recognized as the library")
	}
	if got := sharedEntry("gamma"); got != "" {
		t.Errorf("the library's own entry is offered as shared: %q", got)
	}
	if got := readSharedOwn(); len(got) != 0 {
		t.Errorf("the library's own entries are read as the user's: %v", got)
	}
	ok(t)(SetSkillsWhere(SkillsInShared))
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if l.keepsShared(l.skill("gamma")) {
		t.Error("the setting would link the library into itself")
	}
	ok(t)(Sync())
	if !ours(filepath.Join(h, ".codex/skills/gamma"), "gamma") {
		t.Error("codex, ticked for gamma, has none of it")
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Lstat(filepath.Join(h, ".codex/skills", name)); !os.IsNotExist(err) {
			t.Errorf("codex has %s, which it is not ticked for: %v", name, err)
		}
	}
	for _, name := range []string{"alpha", "gamma"} {
		if !ours(filepath.Join(h, ".claude/skills", name), name) {
			t.Errorf("claude lost %s", name)
		}
	}
	if got, err := SharedSkills(); err != nil || len(got) != 0 {
		t.Errorf("the library's own skills are offered for hand-over: %v %v", got, err)
	}
}

// WardLu (#6699): another skill manager can leave ~/.agents/skills as a
// link to the library's own skills folder. The library's entries must not
// be read as the user's shared skills: each agent gets exactly the skills
// ticked for it, and hand-over has nothing to offer.
func TestSharedFolderIsTheLibrarysOwn(t *testing.T) {
	h := setUpAliasTicks(t)
	linkSharedToLibrary(t, h)
	checkAliasTicks(t, h)
}

func TestSharedIsLibraryNested(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target func() string
	}{
		{"same", skillsDir},
		{"shared inside library", func() string { return filepath.Join(skillsDir(), "nested") }},
		{"shared holds library", func() string { return filepath.Dir(skillsDir()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sandbox(t)
			target := tc.target()
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			shared := filepath.Join(h, ".agents/skills")
			if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := dirLink(target, shared); err != nil {
				t.Fatal(err)
			}
			if !sharedIsLibrary() {
				t.Fatalf("shared %s and library %s are not recognized as one", target, skillsDir())
			}
		})
	}
}

func setUpAliasHandOver(t *testing.T) string {
	t.Helper()
	h := sandbox(t)
	noGoose(t, h)
	ext := filepath.Join(h, "elsewhere/grilling")
	skill(t, ext, "grilling", "Grill")
	src := filepath.Join(h, "my-skills")
	skill(t, filepath.Join(src, "grilling"), "grilling", "Grill")
	skill(t, filepath.Join(src, "chop"), "chop", "Chop")
	ok(t)(InstallSkills(src, []string{"grilling", "chop"}, []string{"claude"}))
	if err := os.RemoveAll(skillDir("grilling")); err != nil {
		t.Fatal(err)
	}
	if err := dirLink(ext, skillDir("grilling")); err != nil {
		t.Fatal(err)
	}
	return h
}

func checkAliasHandOver(t *testing.T, h string) {
	t.Helper()
	ext := filepath.Join(h, "elsewhere/grilling")
	src := filepath.Join(h, "my-skills")
	if _, err := HandOverSkills([]string{"grilling", "chop"}); err == nil || !strings.Contains(err.Error(), "library's own folder") {
		t.Fatalf("hand-over into the library's own folder: %v", err)
	}
	if !linked(skillDir("grilling")) || realDir(skillDir("grilling")) != realDir(ext) {
		t.Error("grilling's library entry went")
	}
	if realDir(skillDir("chop")) != realDir(filepath.Join(src, "chop")) {
		t.Error("chop's library entry went")
	}
	if _, err := os.Stat(filepath.Join(ext, "SKILL.md")); err != nil {
		t.Errorf("the folder kept elsewhere went: %v", err)
	}
	agent := filepath.Join(h, ".claude/skills/grilling")
	if !linked(agent) || realDir(agent) != realDir(ext) {
		t.Error("claude's link to the folder elsewhere went")
	}
	if got, err := SharedSkills(); err != nil || len(got) != 0 {
		t.Errorf("the library's own skills are offered for hand-over: %v %v", got, err)
	}
}

// WardLu (#6697): hand-over used to take the library's own entry away
// with the link in the alias, leaving the agent's link dangling.
func TestHandOverIntoItsOwnFolder(t *testing.T) {
	h := setUpAliasHandOver(t)
	linkSharedToLibrary(t, h)
	checkAliasHandOver(t, h)
}

func TestHandBackIntoItsOwnFolder(t *testing.T) {
	h := sandbox(t)
	ok(t)(CreateSkill("owned", "Owned", nil))
	linkSharedToLibrary(t, h)
	if _, err := HandBackSkill("owned"); err == nil || !strings.Contains(err.Error(), "library's own folder") {
		t.Fatalf("hand-back into the library's own folder: %v", err)
	}
	lib := skillDir("owned")
	if linked(lib) {
		t.Fatal("the library's own folder became a link")
	}
	if _, err := os.Stat(filepath.Join(lib, "SKILL.md")); err != nil {
		t.Errorf("the library's own folder went: %v", err)
	}
	if realDir(filepath.Join(h, ".agents/skills/owned")) != realDir(lib) {
		t.Error("the alias no longer leads to the library's own folder")
	}
}
