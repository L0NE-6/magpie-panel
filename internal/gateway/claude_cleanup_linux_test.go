//go:build linux

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/testenv"
)

// A wrapper can exec Claude Code into another PID namespace. Its host-side
// process group then ends while the workload lives on. The run tag lets a
// cleanup hook kill that workload after the host tree is gone.
func TestClaudeRunCleanupHookReachesDetachedProcess(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid is not installed")
	}

	const runID = "magpie-claude-7747"
	dir := t.TempDir()
	tmp := filepath.Join(dir, runID)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "orphan.pid")
	tagFile := filepath.Join(dir, "run-id")
	hookLog := filepath.Join(dir, "hook.log")
	t.Setenv("ORPHAN_PID_FILE", pidFile)
	t.Setenv("RUN_ID_FILE", tagFile)
	t.Setenv("HOOK_LOG", hookLog)

	hook := filepath.Join(dir, "cleanup")
	testenv.Program(t, hook, `#!/bin/sh
printf '%s\n' "$MAGPIE_CLAUDE_RUN_ID" > "$HOOK_LOG"
kill -9 "$(cat "$ORPHAN_PID_FILE")"
`)
	t.Setenv("MAGPIE_CLAUDE_KILL", hook)

	claude := filepath.Join(dir, "claude")
	testenv.Program(t, claude, `#!/bin/sh
printf '%s\n' "$MAGPIE_CLAUDE_RUN_ID" > "$RUN_ID_FILE"
setsid sh -c 'printf "%s\n" "$$" > "$ORPHAN_PID_FILE"; trap "" HUP INT TERM; exec sleep 300' >/dev/null 2>&1 &
trap 'exit 0' TERM
while :; do sleep 1; done
`)
	cmd := proc.Command("/bin/sh", "-c", "exec "+claude)
	cmd.Env = append(os.Environ(), "MAGPIE_CLAUDE_RUN_ID="+runID)
	run := &subscriptionRun{cmd: cmd, tmp: tmp}
	if err := run.launch(); err != nil {
		t.Fatal(err)
	}

	pid := 0
	t.Cleanup(func() {
		if pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		run.mu.Lock()
		tree := run.tree
		run.mu.Unlock()
		if tree != nil {
			tree.Kill()
		}
		select {
		case <-run.exited:
		case <-time.After(2 * time.Second):
		}
	})

	waitFor(t, "the detached process's pid", func() bool {
		b, err := os.ReadFile(pidFile)
		return err == nil && strings.HasSuffix(string(b), "\n")
	})
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		t.Fatalf("orphan pid %q: %v", b, err)
	}
	waitFor(t, "the run id in Claude Code's environment", func() bool {
		b, err := os.ReadFile(tagFile)
		return err == nil && strings.TrimSpace(string(b)) == runID
	})
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("the detached process %d ended before the hook ran: %v", pid, err)
	}

	select {
	case <-run.abort():
	case <-time.After(5 * time.Second):
		t.Fatal("abort did not return")
	}
	waitFor(t, "the cleanup hook's run id", func() bool {
		b, err := os.ReadFile(hookLog)
		return err == nil && strings.TrimSpace(string(b)) == runID
	})
	waitFor(t, "the detached process to end", func() bool {
		return syscall.Kill(pid, 0) != nil
	})
}
