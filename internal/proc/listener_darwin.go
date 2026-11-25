package proc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// listeningOn asks lsof, which lists this user's processes.
func listeningOn(ctx context.Context, target listenTarget) ([]int, error) {
	lsof := "/usr/sbin/lsof"
	if _, err := os.Stat(lsof); err != nil {
		lsof = "lsof"
	}
	out, err := CommandContext(ctx, lsof, "-nP", "-a", "-iTCP:"+strconv.Itoa(target.port), "-sTCP:LISTEN", "-Fpn").Output()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && len(bytes.TrimSpace(out)) == 0) {
		return nil, err
	}
	// lsof says 1 for none found
	return pidsMatching(lsofListeners(string(out)), target), nil
}

// executable reads kern.procargs2: the argument count, then the path the
// process was started from.
func executable(pid int) (string, error) {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	if len(b) < 5 {
		return "", errors.New("no program path")
	}
	b = b[4:]
	if i := bytes.IndexByte(b, 0); i > 0 {
		return string(b[:i]), nil
	}
	return "", errors.New("no program path")
}

// processName is the program's name by ps, which names any user's process
// where the path (kern.procargs2) is denied.
func processName(pid int) string {
	out, err := CommandContext(context.Background(), "ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
