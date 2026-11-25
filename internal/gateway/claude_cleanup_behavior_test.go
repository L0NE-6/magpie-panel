package gateway

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/proc"
)

// A one-off run has the same cleanup path as a bridge run. This test fails
// on the old code because runClaudeTree returned before running the hook.
func TestClaudeRunTreeRunsCleanupHook(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "hook.log")
	t.Setenv("HOOK_LOG", logFile)
	var cmd *exec.Cmd
	var hook string
	if runtime.GOOS == "windows" {
		cmd = proc.Command("cmd.exe", "/c", "exit 0")
		hook = `echo hook>%HOOK_LOG%`
	} else {
		cmd = proc.Command("/bin/sh", "-c", "exit 0")
		hook = `echo hook > "$HOOK_LOG"`
	}
	t.Setenv("MAGPIE_CLAUDE_KILL", hook)
	cmd.Env = append(os.Environ(), "MAGPIE_CLAUDE_RUN_ID=magpie-claude-7747")
	if err := runClaudeTree(context.Background(), cmd, "a cleanup-hook test"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(logFile)
	if err != nil || strings.TrimSpace(string(b)) != "hook" {
		t.Fatalf("the one-off cleanup hook did not run: %q, %v", b, err)
	}
}
