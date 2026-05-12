// Package scanner discovers running Claude Code processes and their working directories.
package scanner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/platform"
)

// ProcessInfo holds metadata about a running Claude Code process.
type ProcessInfo struct {
	PID             int
	CWD             string
	Uptime          int64 // seconds
	Command         string
	ClaudeConfigDir string // value of CLAUDE_CONFIG_DIR in the process env, or "" for default
}

var claudeConfigDirRE = regexp.MustCompile(`CLAUDE_CONFIG_DIR=(\S+)`)

// getClaudeConfigDir reads CLAUDE_CONFIG_DIR from a process's environment.
// On Linux it reads /proc/{pid}/environ; on macOS it uses ps ewww.
// Returns "" when not found (caller uses the default ~/.claude path).
func getClaudeConfigDir(pid int) string {
	if platform.IsLinux {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
		if err != nil {
			return ""
		}
		for _, kv := range strings.Split(string(raw), "\x00") {
			if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
				return strings.TrimPrefix(kv, "CLAUDE_CONFIG_DIR=")
			}
		}
		return ""
	}
	// macOS: ps ewww appends env vars after the command arguments.
	out, err := exec.Command("ps", "ewww", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // pid from ps output
	if err != nil {
		return ""
	}
	if m := claudeConfigDirRE.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// ParseElapsedTime converts ps etime format (e.g. "2-01:05:30") to seconds.
// Format: [[DD-]HH:]MM:SS — reversed after normalizing separators.
func ParseElapsedTime(etime string) int64 {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(etime), "-", ":"), ":")
	// Reverse so parts[0]=seconds, parts[1]=minutes, parts[2]=hours, parts[3]=days
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	multipliers := []int64{1, 60, 3600, 86400}
	var total int64
	for i, p := range parts {
		if i >= len(multipliers) {
			break
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		total += n * multipliers[i]
	}
	return total
}

// ParseLsofBatch parses `lsof -a -d cwd -Fn` output into a pid→cwd map.
// Output format per process: p<pid>\nn<path>\n
func ParseLsofBatch(stdout string) map[int]string {
	result := make(map[int]string)
	var currentPID int
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "p") {
			pid, err := strconv.Atoi(strings.TrimPrefix(line, "p"))
			if err == nil {
				currentPID = pid
			}
		} else if strings.HasPrefix(line, "n") && currentPID != 0 {
			result[currentPID] = strings.TrimPrefix(line, "n")
			currentPID = 0
		}
	}
	return result
}

// ProjectName returns the last path component of a CWD.
func ProjectName(cwd string) string {
	return filepath.Base(cwd)
}

func getCWDsLinux(pids []int) map[int]string {
	result := make(map[int]string)
	for _, pid := range pids {
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		if err == nil {
			result[pid] = target
		}
	}
	return result
}

func getCWDsMac(ctx context.Context, pids []int) map[int]string {
	if len(pids) == 0 {
		return nil
	}
	pidStrs := make([]string, len(pids))
	for i, p := range pids {
		pidStrs[i] = strconv.Itoa(p)
	}
	out, err := exec.CommandContext(ctx, //nolint:gosec // pidStrs are integer PIDs parsed from ps output — not user-controlled input
		"lsof", "-a", "-d", "cwd", "-p", strings.Join(pidStrs, ","), "-Fn",
	).Output()
	if err != nil {
		return nil
	}
	return ParseLsofBatch(string(out))
}

// ScanProcesses returns all running Claude Code processes with their CWDs.
func ScanProcesses(ctx context.Context) ([]ProcessInfo, error) {
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,etime,comm").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}

	type rawProc struct {
		pid     int
		etime   string
		command string
	}

	var raws []rawProc
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		comm := strings.Join(fields[2:], " ")
		if !strings.HasSuffix(comm, "/claude") && comm != "claude" {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		raws = append(raws, rawProc{pid: pid, etime: fields[1], command: comm})
	}

	pids := make([]int, len(raws))
	for i, r := range raws {
		pids[i] = r.pid
	}

	var cwdMap map[int]string
	if platform.IsLinux {
		cwdMap = getCWDsLinux(pids)
	} else {
		cwdMap = getCWDsMac(ctx, pids)
	}

	var result []ProcessInfo
	for _, r := range raws {
		cwd, ok := cwdMap[r.pid]
		if !ok || cwd == "" || cwd == "/" {
			continue
		}
		result = append(result, ProcessInfo{
			PID:             r.pid,
			CWD:             cwd,
			Uptime:          ParseElapsedTime(r.etime),
			Command:         r.command,
			ClaudeConfigDir: getClaudeConfigDir(r.pid),
		})
	}
	return result, nil
}
