package gateway

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/wslrun"
)

// A run's Claude Code gets the tag its cleanup hook reads, but not the hook
// command itself: that command may name a container or host the workload
// does not need.
func TestClaudeCleanupEnvTagsTheRunWithoutTheHook(t *testing.T) {
	env := claudeCleanupEnv([]string{
		"PATH=/bin",
		claudeKillEnv + "=docker exec cell pkill -f $MAGPIE_CLAUDE_RUN_ID",
		claudeRunIDEnv + "=an-old-run",
		"KEEP=yes",
	}, "magpie-claude-7747")

	if slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, claudeKillEnv+"=") }) {
		t.Fatalf("the cleanup command reached Claude Code: %v", env)
	}
	if !slices.Contains(env, claudeRunIDEnv+"=magpie-claude-7747") {
		t.Fatalf("the run tag is missing: %v", env)
	}
	if !slices.Contains(env, "PATH=/bin") || !slices.Contains(env, "KEEP=yes") {
		t.Fatalf("an unrelated variable was dropped: %v", env)
	}
	if got := claudeRunIDFromEnv(env); got != "magpie-claude-7747" {
		t.Fatalf("run id from environment = %q", got)
	}
}

// A non-interactive Claude Code in WSL gets the run tag through WSLENV too.
func TestClaudeRunIDGoesToWSL(t *testing.T) {
	env := claudeCleanupEnv([]string{"PATH=/bin"}, "magpie-claude-7747")
	c := claudeCLI{wsl: &wslrun.Tool{Distro: "Ubuntu", Path: "/usr/bin/claude", PATH: "/usr/bin:/bin"}}
	got := c.env(env, "")
	var wslenv string
	for _, e := range got {
		if strings.HasPrefix(e, "WSLENV=") {
			wslenv = e
		}
	}
	if !strings.Contains(wslenv, claudeRunIDEnv) {
		t.Fatalf("WSLENV does not carry the run tag: %q", wslenv)
	}
}

func TestClaudeCleanupEnvDropsWindowsCaseVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows environment names are case-insensitive")
	}
	env := claudeCleanupEnv([]string{
		"magpie_claude_kill=old command",
		"Magpie_Claude_Run_Id=old run",
		"KEEP=yes",
	}, "magpie-claude-7747")
	if slices.ContainsFunc(env, func(e string) bool {
		k, _, _ := strings.Cut(e, "=")
		return strings.EqualFold(k, claudeKillEnv)
	}) {
		t.Fatalf("the cleanup command reached Claude Code: %v", env)
	}
	if got := claudeRunIDFromEnv(env); got != "magpie-claude-7747" {
		t.Fatalf("run id = %q, want the canonical value", got)
	}
	if !slices.Contains(env, "KEEP=yes") {
		t.Fatalf("an ordinary variable was dropped: %v", env)
	}
}
