package agent

// Claude Code kept in more than one config folder (CLAUDE_CONFIG_DIR, as in
// `alias claude-work='CLAUDE_CONFIG_DIR=~/.claude-work claude'`): the one
// the variable (or ~/.claude) names is the claude row; every other folder
// beside it under the home folder that is named .claude… and holds a
// Claude Code's own files is a row of its own, claude#<name> (the way omp's
// profiles are omp#<name>), with its own settings.json to connect, a model
// to pick and Check. BieJian21872 on X: only the one at the default path
// was found.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/desktopiso"
	"github.com/yetone/magpie/internal/dirent"
)

// claudeDirName is the part of a folder's name after ".claude" as a row's
// name: ".claude-work" is "work", ".claude2" is "2". "" for a name that is
// no extra Claude Code folder (the file ~/.claude.json and its backups, the
// folder .claude itself, the launch file's).
func claudeDirName(base string) string {
	rest, ok := strings.CutPrefix(strings.ToLower(base), ".claude")
	if !ok || rest == "" || strings.HasPrefix(rest, ".") {
		return ""
	}
	rest = strings.Trim(rest, "-_")
	if rest == "" || rest == "launch" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, rest)
}

// claudeDirIsClaude says whether dir holds what Claude Code itself keeps in
// a config folder: its .claude.json, its sign-in file, its projects, or a
// settings.json holding a key Claude Code writes. Another tool's .claude-…
// folder (a router's, a launcher's) has none, and one that does keep a
// settings.json by the name is no Claude Code either: claude-mem's
// ~/.claude-mem holds a settings.json of its own, all CLAUDE_MEM_* keys,
// and was a "Claude Code · mem" row every skill was ticked on (#7749).
func claudeDirIsClaude(dir string) bool {
	for _, f := range []string{".claude.json", ".credentials.json"} {
		if fi, err := os.Stat(filepath.Join(dir, f)); err == nil && fi.Mode().IsRegular() {
			return true
		}
	}
	if isDir(filepath.Join(dir, "projects")) {
		return true
	}
	return claudeSettingsIsClaude(filepath.Join(dir, "settings.json"))
}

// claudeSettingsKeys are the keys of Claude Code's own settings.json, from
// its settings reference, plus the three magpie itself adds (modelPicker,
// modelSettings, autoCompactWindow) so a folder already connected or tuned
// keeps counting as Claude Code's. Another tool's file by the same name
// holds none (#7749).
var claudeSettingsKeys = map[string]bool{
	"apiKeyHelper": true, "attribution": true, "autoCompactEnabled": true,
	"autoCompactWindow": true, "autoUpdates": true, "awsAuthRefresh": true,
	"awsCredentialExport": true, "cleanupPeriodDays": true,
	"companyAnnouncements": true, "disabledMcpjsonServers": true,
	"editorMode": true, "enableAllProjectMcpServers": true,
	"enabledMcpjsonServers": true, "enabledPlugins": true, "env": true,
	"forceLoginMethod": true, "forceLoginOrgUUID": true, "hooks": true,
	"includeCoAuthoredBy": true, "installMethod": true, "language": true,
	"model": true, "modelOverrides": true, "modelPicker": true,
	"modelSettings": true, "outputStyle": true, "permissions": true,
	"plansDirectory": true, "preferredNotifChannel": true, "sandbox": true,
	"showThinkingSummaries": true, "spinnerTipsEnabled": true,
	"statusLine": true, "theme": true, "verbose": true,
}

// claudeSettingsIsClaude says path is Claude Code's settings.json: a JSON
// object holding a key Claude Code writes (claudeSettingsKeys). An empty
// object, a file that doesn't parse and a file that isn't an object count
// as nobody's: another tool's folder by the name has one of those too.
func claudeSettingsIsClaude(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	for k := range m {
		if claudeSettingsKeys[k] {
			return true
		}
	}
	return false
}

// claudeDirAgents are the Claude Code config folders beside the claude
// row's, each an agent of its own. None while the folder has no Claude
// Code's files: a leftover is no agent.
func claudeDirAgents(home string) []*Agent {
	names, byName := claudeDirsIn(home)
	var out []*Agent
	for _, name := range names {
		dir := byName[name]
		id := "claude#" + name
		a := claudeIn(place{home: home, id: id, dir: dir})
		a.ID, a.Name = id, "Claude Code · "+name
		// the isolated Claude Desktop's Code tab's (#6459), by its app's name
		if dir == desktopIsolatedClaudeDir(home) {
			a.Name = "Claude Code · " + desktopiso.Name
		}
		// the claude row keeps the command, the names and the requests
		a.Aliases, a.UA, a.Bin = nil, nil, ""
		a.detect = func() bool {
			return claudeDirIsClaude(dir) || dir == desktopIsolatedClaudeDir(home) && isFile(filepath.Join(dir, "settings.json"))
		}
		out = append(out, a)
	}
	return out
}

// claudeDirsIn are the Claude Code config folders beside the claude row's
// (claudeDirAgents), by name in order, and each one's folder by its name.
func claudeDirsIn(home string) ([]string, map[string]string) {
	es, err := os.ReadDir(home)
	if err != nil {
		return nil, nil
	}
	main := appdir.ClaudeDirIn(home)
	// the isolated Claude Desktop's Code tab folder is Claude Code's by
	// construction (magpie made it for Claude Code, #6459): its
	// settings.json starts as {}, which holds no key of Claude Code's yet
	// (#7749)
	iso := desktopIsolatedClaudeDir(home)
	isClaude := func(dir string) bool {
		return claudeDirIsClaude(dir) || dir == iso && isFile(filepath.Join(dir, "settings.json"))
	}
	var names []string
	byName := map[string]string{}
	for _, e := range es {
		name := claudeDirName(e.Name())
		dir := filepath.Join(home, e.Name())
		if name == "" || !dirent.IsFolder(home, e) || dir == main || !isClaude(dir) {
			continue
		}
		if _, dup := byName[name]; dup {
			continue
		}
		byName[name] = dir
		names = append(names, name)
	}
	// with CLAUDE_CONFIG_DIR elsewhere, the default folder is one of the others
	if def := filepath.Join(home, ".claude"); def != main && isDir(def) && claudeDirIsClaude(def) {
		byName["default"] = def
		names = append(names, "default")
	}
	sort.Strings(names)
	return names, byName
}
