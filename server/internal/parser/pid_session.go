package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PidSession is the authoritative per-process session record Claude Code writes
// to <configDir>/sessions/{pid}.json. It binds a running PID to its exact
// session ID — the only collision-free signal when several agents run in one
// project directory.
type PidSession struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	Entrypoint string `json:"entrypoint"`
	Status     string `json:"status"`
	Version    string `json:"version"`
}

// ReadPidSession reads <configDir>/sessions/{pid}.json from the first config
// dir that has it, returning the parsed record. ok is false when no config dir
// holds a readable, session-bearing file for the pid.
func ReadPidSession(pid int, configDirs []string) (*PidSession, bool) {
	name := strconv.Itoa(pid) + ".json"
	for _, dir := range configDirs {
		b, err := os.ReadFile(filepath.Join(dir, "sessions", name))
		if err != nil {
			continue
		}
		var ps PidSession
		if err := json.Unmarshal(b, &ps); err != nil || ps.SessionID == "" {
			continue
		}
		return &ps, true
	}
	return nil, false
}

// SessionIDFromArgs extracts a session UUID from a process command line, looking
// at --resume / --session-id (both "--flag value" and "--flag=value" forms).
// Returns "" when no valid UUID-shaped value is present.
func SessionIDFromArgs(command string) string {
	fields := strings.Fields(command)
	for i, f := range fields {
		var val string
		switch {
		case f == "--resume" || f == "--session-id" || f == "-r":
			if i+1 < len(fields) {
				val = fields[i+1]
			}
		case strings.HasPrefix(f, "--resume="):
			val = strings.TrimPrefix(f, "--resume=")
		case strings.HasPrefix(f, "--session-id="):
			val = strings.TrimPrefix(f, "--session-id=")
		}
		if uuidRE.MatchString(val) {
			return val
		}
	}
	return ""
}

// SessionRequest carries everything needed to resolve the session bound to one
// running process.
type SessionRequest struct {
	CWD             string
	PID             int
	Command         string // full process command line (argv), used for --resume
	UptimeSeconds   int64
	ClaudeConfigDir string // value of CLAUDE_CONFIG_DIR from the process env, or ""
}

// ResolveSessionForProcess returns the SessionData bound to a specific process.
//
// Precedence:
//  1. <configDir>/sessions/{pid}.json — authoritative PID→session mapping.
//  2. --resume / --session-id in the process args.
//  3. directory mtime heuristic, excluding any session ID already in `claimed`.
//
// When `claimed` is non-nil the resolved session ID is recorded in it, so a
// caller iterating the processes of one project directory keeps every agent on
// a distinct session. This is the fix for "every session in a folder shows the
// same content": resolution is now per-PID, not per-directory.
func ResolveSessionForProcess(req SessionRequest, claimed map[string]bool) (*SessionData, error) {
	forcedID := ""
	if ps, ok := ReadPidSession(req.PID, candidateConfigDirs(req.ClaudeConfigDir)); ok {
		forcedID = ps.SessionID
	}
	if forcedID == "" {
		forcedID = SessionIDFromArgs(req.Command)
	}
	// A forced ID already claimed by another process means our pid file is stale
	// or two processes name the same session; drop the hint and let the
	// claimed-aware heuristic pick a distinct file instead.
	if forcedID != "" && claimed[forcedID] {
		forcedID = ""
	}

	data, err := findSessionForProjectFiltered(req.CWD, req.PID, req.UptimeSeconds, req.ClaudeConfigDir, claimed, forcedID)
	if err != nil {
		return nil, err
	}
	if claimed != nil {
		claimed[data.SessionID] = true
	}
	return data, nil
}

// candidateConfigDirs returns the process's own config dir first (when set),
// followed by all auto-detected Claude config dirs, de-duplicated.
func candidateConfigDirs(procConfigDir string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(procConfigDir)
	for _, d := range allClaudeConfigDirs() {
		add(d)
	}
	return out
}

// filterToID returns only the candidate whose file is <sessionID>.jsonl.
func filterToID(candidates []sessionFileCandidate, sessionID string) []sessionFileCandidate {
	target := sessionID + ".jsonl"
	for _, c := range candidates {
		if filepath.Base(c.path) == target {
			return []sessionFileCandidate{c}
		}
	}
	return nil
}

// filterOutClaimed drops candidates whose session ID is already claimed.
func filterOutClaimed(candidates []sessionFileCandidate, claimed map[string]bool) []sessionFileCandidate {
	out := candidates[:0:0]
	for _, c := range candidates {
		id := strings.TrimSuffix(filepath.Base(c.path), ".jsonl")
		if claimed[id] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// locateSessionFile finds <configDir>/projects/*/<sessionID>.jsonl across the
// given config dirs (used when a pinned session is not under cwd's own encoded
// project directory).
func locateSessionFile(sessionID string, configDirs []string) (sessionFileCandidate, bool) {
	name := sessionID + ".jsonl"
	for _, dir := range configDirs {
		projectsDir := filepath.Join(dir, "projects")
		entries, err := os.ReadDir(projectsDir)
		if err != nil {
			continue
		}
		for _, d := range entries {
			if !d.IsDir() {
				continue
			}
			p := filepath.Join(projectsDir, d.Name(), name)
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			return sessionFileCandidate{path: p, mtime: info.ModTime(), inode: inodeOf(info)}, true
		}
	}
	return sessionFileCandidate{}, false
}
