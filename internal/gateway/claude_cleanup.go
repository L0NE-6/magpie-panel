package gateway

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/claudetui"
	"github.com/yetone/magpie/internal/proc"
)

const (
	// claudeKillEnv names a command run after a Claude Code run's host
	// process tree ends. It reaches a workload behind docker exec, ssh, WSL
	// or another exec boundary that the host process group cannot cross.
	claudeKillEnv = "MAGPIE_CLAUDE_KILL"
	// claudeRunIDEnv tags the run in Claude Code's environment. The cleanup
	// command reads it to find only this run's carriers.
	claudeRunIDEnv = claudetui.RunIDEnv
)

// claudeCleanupWait bounds a cleanup hook, including a docker exec that has
// to reach a container.
var claudeCleanupWait = 10 * time.Second

// claudeCleanupEnv gives Claude Code the run's tag but not the command that
// may hold the operator's container or host details.
func claudeCleanupEnv(env []string, runID string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if claudeEnvKey(k, claudeKillEnv) || claudeEnvKey(k, claudeRunIDEnv) {
			continue
		}
		out = append(out, e)
	}
	return append(out, claudeRunIDEnv+"="+runID)
}

// claudeRunIDFromEnv reads the last value a command's environment gives the
// run tag, as os.Getenv would read the process's own environment.
func claudeRunIDFromEnv(env []string) string {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && claudeEnvKey(k, claudeRunIDEnv) {
			return v
		}
	}
	return ""
}

func claudeEnvKey(k, want string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(k, want)
	}
	return k == want
}

// runCleanup runs the optional command after the host-side run has ended.
// The command's stdout and stderr go to app.log so a failed cleanup is not
// silent.
func (r *subscriptionRun) runCleanup() {
	if r.tmp != "" {
		claudeCleanup(filepath.Base(r.tmp))
	}
}

// claudeCleanup runs the optional command after a Claude Code run has ended.
// runID names only that run; an empty one means the run had no safe tag.
func claudeCleanup(runID string) {
	if runID == "" {
		return
	}
	command := strings.TrimSpace(os.Getenv(claudeKillEnv))
	if command == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeCleanupWait)
	defer cancel()
	cmd := claudeCleanupCommand(ctx, command)
	cmd.Env = claudeCleanupEnv(os.Environ(), runID)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	tree, err := proc.StartTree(cmd)
	if err != nil {
		log.Printf("claude: cleanup hook for %s didn't start: %s", runID, logSafe(err.Error()))
		return
	}
	err = tree.Wait()
	said := strings.TrimSpace(out.String())
	if err == nil {
		if said != "" {
			log.Printf("claude: cleanup hook for %s: %s", runID, logSafe(said))
		}
		return
	}
	why := err.Error()
	if said != "" {
		why += ": " + said
	}
	log.Printf("claude: cleanup hook for %s failed: %s", runID, logSafe(why))
}

// waitForCleanup closes once both the host-side stop and the run's cleanup
// hook have finished.
func waitForCleanup(stopped, cleanupDone <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		<-stopped
		<-cleanupDone
		close(done)
	}()
	return done
}
