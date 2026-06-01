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

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/platform"
)

// ProcessInfo holds metadata about a running Claude Code process.
type ProcessInfo struct {
	PID             int
	CWD             string
	Uptime          int64 // seconds
	Command         string
	ClaudeConfigDir string       // value of CLAUDE_CONFIG_DIR in the process env, or "" for default
	Provider        sdk.Provider // detected AI coding CLI provider; defaults to ProviderClaude
}

var (
	claudeConfigDirRE = regexp.MustCompile(`CLAUDE_CONFIG_DIR=(\S+)`)
	pidFieldRE        = regexp.MustCompile(`^\s*(\d+)\s`)
)

// getClaudeConfigDirsBatch fetches CLAUDE_CONFIG_DIR for all given PIDs.
// On Linux, reads /proc/{pid}/environ per-PID (file reads, no subprocess).
// On macOS, issues a single `ps ewww -p pid1,pid2,...` call instead of one per PID.
func getClaudeConfigDirsBatch(pids []int) map[int]string {
	result := make(map[int]string, len(pids))
	if platform.IsLinux {
		for _, pid := range pids {
			raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
			if err != nil {
				continue
			}
			for _, kv := range strings.Split(string(raw), "\x00") {
				if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
					result[pid] = strings.TrimPrefix(kv, "CLAUDE_CONFIG_DIR=")
					break
				}
			}
		}
		return result
	}
	if len(pids) == 0 {
		return result
	}
	// macOS: one ps call for all PIDs — `ps ewww` prints full env after command,
	// one line per process (www = no truncation). Each line starts with the PID.
	pidStrs := make([]string, len(pids))
	for i, p := range pids {
		pidStrs[i] = strconv.Itoa(p)
	}
	out, err := exec.Command("ps", "ewww", "-p", strings.Join(pidStrs, ",")).Output() //nolint:gosec // pid ints formatted as strings
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(out), "\n") {
		m := pidFieldRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pid, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if cm := claudeConfigDirRE.FindStringSubmatch(line); cm != nil {
			result[pid] = string(cm[1])
		}
	}
	return result
}

// ParseElapsedTime converts ps etime format (e.g. "2-01:05:30") to seconds.
// Format: [[DD-]HH:]MM:SS
// The day component (before "-") is parsed independently so that "1-" yields
// 86400 rather than 60 (which would happen if "-" were blindly replaced by ":").
func ParseElapsedTime(etime string) int64 {
	etime = strings.TrimSpace(etime)
	if etime == "" {
		return 0
	}

	var days int64
	if idx := strings.Index(etime, "-"); idx != -1 {
		days, _ = strconv.ParseInt(etime[:idx], 10, 64)
		etime = etime[idx+1:]
	}

	// Remaining: [[HH:]MM:]SS — split by ":" and reverse so index 0 = seconds.
	parts := strings.Split(etime, ":")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	multipliers := []int64{1, 60, 3600}
	var total int64
	for i, p := range parts {
		if i >= len(multipliers) {
			break
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		total += n * multipliers[i]
	}
	return days*86400 + total
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
	// Use `args` (full command line) rather than `comm` so flags like
	// `--resume <sessionId>` survive — the merger needs them to bind a process
	// to its exact session. DetectProviderFromCommand strips back to argv[0].
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid,etime,args").Output()
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
		if DetectProviderFromCommand(comm) == "" {
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

	// Batch-fetch CLAUDE_CONFIG_DIR for all PIDs in one call (macOS: one ps subprocess).
	configDirs := getClaudeConfigDirsBatch(pids)

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
			ClaudeConfigDir: configDirs[r.pid],
			Provider:        DetectProviderFromCommand(r.command),
		})
	}
	return result, nil
}

// DetectProviderFromCommand maps a process command name to a provider.
// Matches both bare names (e.g. "claude") and absolute paths
// (e.g. "/usr/local/bin/codex"). Returns "" when the command does not
// belong to any supported AI coding CLI.
func DetectProviderFromCommand(comm string) sdk.Provider {
	comm = strings.TrimSpace(comm)
	if comm == "" {
		return ""
	}
	// Strip arguments — only look at argv[0].
	if i := strings.IndexByte(comm, ' '); i >= 0 {
		comm = comm[:i]
	}
	base := filepath.Base(comm)
	switch base {
	case "claude":
		return sdk.ProviderClaude
	case "codex":
		return sdk.ProviderCodex
	case "gemini":
		return sdk.ProviderGemini
	}
	return ""
}
