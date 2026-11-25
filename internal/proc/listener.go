package proc

import (
	"context"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// The process listening on a port, and its program: how the GUI finds the
// older magpie that keeps the gateway's port and quits it, when the user
// asks it to (leslie_luo on Discord: the page said an older magpie served
// the gateway, with no way to find it and quit it).

// ErrDenied is a process this user may not end: another user's, or one
// run as administrator.
var ErrDenied = errors.New("not allowed to end it")

// ListeningOn is the ids of the processes listening on TCP port, that this
// user can see: lsof on a Mac, /proc on Linux, Get-NetTCPConnection on
// Windows. Another user's process may be missing. Use ListeningOnAddr when
// the local address matters, as it does for the gateway's take-over.
func ListeningOn(ctx context.Context, port int) ([]int, error) {
	return listeningOn(ctx, listenTarget{port: port, any: true})
}

// ListeningOnAddr is ListeningOn for one local address. The address is part
// of the match: another program listening on the same port at another
// address is not a holder of this one.
func ListeningOnAddr(ctx context.Context, addr string) ([]int, error) {
	target, err := resolveListenTarget(addr)
	if err != nil {
		return nil, err
	}
	return listeningOn(ctx, target)
}

// listenTarget is the local address a listener query must match. A
// wildcard target matches a wildcard listener, while a wildcard listener
// matches any target it covers.
type listenTarget struct {
	ip       net.IP
	port     int
	wildcard bool
	any      bool
}

func resolveListenTarget(addr string) (listenTarget, error) {
	tcp, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return listenTarget{}, fmt.Errorf("listener address %q: %w", addr, err)
	}
	if tcp.Port < 0 || tcp.Port > 65535 {
		return listenTarget{}, fmt.Errorf("listener address %q: port out of range", addr)
	}
	ip := normalizedIP(tcp.IP)
	return listenTarget{
		ip:       ip,
		port:     tcp.Port,
		wildcard: ip == nil || ip.IsUnspecified(),
	}, nil
}

func (t listenTarget) matches(host string, port int) bool {
	if port != t.port {
		return false
	}
	if t.any {
		return true
	}
	if host == "*" {
		// lsof names a socket on every interface with a star.
		return true
	}
	ip := normalizedIP(net.ParseIP(strings.Trim(host, "[]")))
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() {
		if ip.To4() != nil {
			return t.ip == nil || t.ip.To4() != nil
		}
		// An IPv6 wildcard is dual-stack on the systems this runs on.
		return true
	}
	if t.wildcard {
		if t.ip == nil {
			return true
		}
		if t.ip.To4() != nil {
			return ip.To4() != nil
		}
		// An IPv6 wildcard is dual-stack on the systems this runs on.
		return true
	}
	return ip.Equal(t.ip)
}

func normalizedIP(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

// listenerRecord is one address a process was seen listening on.
type listenerRecord struct {
	pid  int
	host string
	port int
}

// pidsMatching keeps the processes listening on target, once each.
func pidsMatching(records []listenerRecord, target listenTarget) []int {
	var ps []int
	seen := map[int]bool{}
	for _, r := range records {
		if r.pid > 0 && !seen[r.pid] && target.matches(r.host, r.port) {
			seen[r.pid] = true
			ps = append(ps, r.pid)
		}
	}
	return ps
}

// lsofListeners reads the pid and name records of lsof -Fpn.
func lsofListeners(out string) []listenerRecord {
	var records []listenerRecord
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			host, port, err := net.SplitHostPort(line[1:])
			if err != nil {
				continue
			}
			n, err := strconv.Atoi(port)
			if err == nil {
				records = append(records, listenerRecord{pid: pid, host: host, port: n})
			}
		}
	}
	return records
}

// windowsListeners reads the pid and LocalAddress records of
// Get-NetTCPConnection, one pid|address per line.
func windowsListeners(out string, port int) []listenerRecord {
	var records []listenerRecord
	for _, line := range strings.Split(out, "\n") {
		pidText, host, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidText)
		if err == nil && pid > 0 && host != "" {
			records = append(records, listenerRecord{pid: pid, host: host, port: port})
		}
	}
	return records
}

// listenInodes are the inodes of the sockets listening (st 0A) on target
// in a /proc/net/tcp table.
func listenInodes(table string, target listenTarget) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(table, "\n")[1:] {
		f := strings.Fields(l)
		if len(f) < 10 || f[3] != "0A" {
			continue
		}
		host, port, ok := procListenAddress(f[1])
		if !ok || !target.matches(host, port) {
			continue
		}
		if f[9] != "0" {
			out[f[9]] = true
		}
	}
	return out
}

func procListenAddress(s string) (string, int, bool) {
	ipText, portText, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, false
	}
	port, err := strconv.ParseUint(portText, 16, 16)
	if err != nil {
		return "", 0, false
	}
	ip := procIP(ipText)
	if ip == nil {
		return "", 0, false
	}
	return ip.String(), int(port), true
}

// procIP reads the kernel's address: four bytes of an IPv4 address in host
// order, or four host-order words for IPv6.
func procIP(s string) net.IP {
	switch len(s) {
	case net.IPv4len * 2:
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil
		}
		slices.Reverse(b)
		return net.IP(b)
	case net.IPv6len * 2:
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil
		}
		for i := 0; i < len(b); i += 4 {
			slices.Reverse(b[i : i+4])
		}
		return net.IP(b)
	default:
		return nil
	}
}

// Executable is the path of the program process pid runs, as the system
// says it: the one it was started from, also when that file has been
// replaced since (an update).
func Executable(pid int) (string, error) {
	p, err := executable(pid)
	if err != nil {
		return "", err
	}
	// Linux names a replaced file so
	return strings.TrimSuffix(p, " (deleted)"), nil
}

// Terminate asks process pid to end: SIGTERM on Unix, which a magpie quits
// on; on Windows it is ended at once (nothing else reaches a tray app).
// ErrDenied when this user may not.
func Terminate(pid int) error { return terminate(pid) }

// Kill ends process pid at once. ErrDenied when this user may not.
func Kill(pid int) error { return kill(pid) }

// RunsMagpie says whether process pid, running the program at path (as
// Executable says), is a magpie: by the program's name (IsMagpie), or else
// by what the program is, magpie's own main package, read from the build
// information Go writes into it. A magpie under a name of the user's
// (acezan on Discord: magpie.real, behind a wrapper script installed as
// magpie) is still one, and on Linux this holds for one whose file an
// update has since replaced, read through /proc/<pid>/exe: the panel had
// called that magpie "not magpie" and left it holding the gateway's port.
func RunsMagpie(pid int, path string) bool {
	if IsMagpie(path) {
		return true
	}
	file := path
	if runtime.GOOS == "linux" && pid > 0 {
		file = filepath.Join("/proc", strconv.Itoa(pid), "exe")
	}
	if file == "" {
		return false
	}
	bi, err := buildinfo.ReadFile(file)
	return err == nil && bi.Path == magpieMain
}

// magpieMain is magpie's main package, as a build of it names itself; a
// var for the tests, whose own binary is another package's.
var magpieMain = "github.com/yetone/magpie"

// IsMagpie says whether the program at path is a magpie: magpie, its
// magpie-* builds or dial, its name before; with .exe, and Windows' .old
// for one replaced by an update while it ran, or .old-2 when that one was
// still held; and magpie.real, the program a wrapper script named magpie
// runs (acezan on Discord).
func IsMagpie(path string) bool {
	if path == "" {
		return false
	}
	n := strings.ToLower(filepath.Base(filepath.Clean(strings.ReplaceAll(path, `\`, "/"))))
	// an update moves a running exe aside to exe.old, and to exe.old-2, -3
	// when that one is held too, so the tail is a number as often as not
	if i := strings.LastIndex(n, ".old"); i > 0 && isOldCount(n[i+len(".old"):]) {
		n = n[:i]
	}
	n = strings.TrimSuffix(n, ".old")
	n = strings.TrimSuffix(n, ".new")
	n = strings.TrimSuffix(n, ".exe")
	n = strings.TrimSuffix(n, ".real")
	for _, name := range []string{"magpie", "dial"} {
		if n == name || strings.HasPrefix(n, name+"-") || strings.HasPrefix(n, name+"_") {
			return true
		}
	}
	return false
}

// isOldCount is the -2, -3 an update appends when exe.old is taken too: a
// number alone is magpie's own naming, so another program's is left alone.
func isOldCount(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// IsForwarder says whether the program at path holds a port for a container
// or a VM, which serves on it from inside: OrbStack's helper, Docker
// Desktop's backend and vpnkit, docker-proxy, Podman's gvproxy, Lima's
// (Colima's, Rancher Desktop's) host agent, WSL's relay. A magpie answering
// there runs in that container (leslie_luo on Discord: magpie v0.1.552 in
// OrbStack held 3425, and its helper was named as the program to quit),
// which only the container's own tools stop.
func IsForwarder(path string) bool {
	if path == "" {
		return false
	}
	p := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	for _, app := range []string{"/orbstack.app/", "/docker.app/", "/rancher desktop.app/", "/podman desktop.app/", "/docker/resources/"} {
		if strings.Contains(p, app) {
			return true
		}
	}
	n := strings.TrimSuffix(filepath.Base(filepath.Clean(p)), ".exe")
	for _, name := range []string{"orbstack", "com.docker.", "docker-proxy", "dockerd", "vpnkit", "gvproxy", "podman", "limactl", "lima-", "wslrelay", "wslhost", "rootlesskit", "slirp4netns"} {
		if strings.HasPrefix(n, name) {
			return true
		}
	}
	return false
}
