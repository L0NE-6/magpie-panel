//go:build windows

package gateway

import (
	"context"
	"os/exec"

	"github.com/yetone/magpie/internal/proc"
)

func claudeCleanupCommand(ctx context.Context, command string) *exec.Cmd {
	return proc.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
}
