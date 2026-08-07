// Package procenv reads environment variables out of running processes.
//
// A session started from a shell wrapper carries the profile it was started
// with in its own environment (CLAUDE_CONFIG_DIR being the one that separates
// a work profile from a personal one). Reading it back from the live process is
// exact for every session, however it was launched — no inference from session
// files, which cannot distinguish two config dirs that symlink to one store.
package procenv

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/platform"
)

// psPath is absolute on both supported platforms; only the macOS branch uses it.
const psPath = "/bin/ps"

// Lookup returns the value of key for each pid that has it set. Pids that are
// gone, owned by another user, or simply do not set key are absent from the
// result rather than reported as an error: the caller treats "unknown" and
// "unset" the same way.
// The read runs inside the SSE broadcast tick, which is a single goroutine that
// also drives the heartbeat: an unbounded `ps` would stall every connected
// client for as long as it hangs, so the call is always given a deadline.
const lookupTimeout = 2 * time.Second

func Lookup(ctx context.Context, pids []int, key string) map[int]string {
	if len(pids) == 0 || key == "" {
		return nil
	}
	if platform.IsLinux {
		return lookupProc(pids, key)
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	return lookupPS(ctx, pids, key)
}

// lookupProc reads /proc/<pid>/environ, whose entries are NUL-separated.
func lookupProc(pids []int, key string) map[int]string {
	prefix := key + "="
	out := make(map[int]string)
	for _, pid := range pids {
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
		if err != nil {
			continue
		}
		for entry := range bytes.SplitSeq(raw, []byte{0}) {
			if v, ok := strings.CutPrefix(string(entry), prefix); ok {
				out[pid] = v
				break
			}
		}
	}
	return out
}

// lookupPS asks ps for the environment of all pids in one call. macOS has no
// /proc, and `ps eww` prints `KEY=value` pairs after the command; values with
// spaces are therefore indistinguishable from the next pair, which is fine for
// the path-shaped variables this is used for.
func lookupPS(ctx context.Context, pids []int, key string) map[int]string {
	args := make([]string, 0, len(pids)+3)
	args = append(args, "eww", "-o", "pid=,command=", "-p")
	list := make([]string, len(pids))
	for i, pid := range pids {
		list[i] = strconv.Itoa(pid)
	}
	args = append(args, strings.Join(list, ","))

	// Absolute path: the server inherits a PATH that on a dev machine routinely
	// contains user-writable directories, and this runs every broadcast tick.
	raw, err := exec.CommandContext(ctx, psPath, args...).Output()
	if err != nil && len(raw) == 0 {
		slog.Debug("procenv: ps lookup failed", "err", err)
		return nil
	}
	return parsePSOutput(raw, key)
}

func parsePSOutput(raw []byte, key string) map[int]string {
	prefix := key + "="
	out := make(map[int]string)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(f, prefix); ok {
				out[pid] = v
				break
			}
		}
	}
	// An over-long line aborts the scan, silently dropping every pid printed
	// after it — which downstream reads as "variable unset", not as a failure.
	if err := scanner.Err(); err != nil {
		slog.Warn("procenv: ps output truncated, some pids unresolved", "err", err)
	}
	return out
}
