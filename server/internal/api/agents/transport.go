package agents

import (
	"fmt"
	"strconv"
	"strings"
)

// headlessTransport is the live-injection transport for a server-spawned
// (terminal-less) agent.
type headlessTransport int

const (
	transportTmux headlessTransport = iota
	transportPTY
)

// selectHeadlessTransport picks tmux when it is on PATH, else the pty broker.
// tmuxPath is exec.LookPath("tmux") ("" on failure). Unlike `agent-dashboard
// live` we never reuse the server's own $TMUX pane — a spawned agent always
// gets a fresh detached session.
func selectHeadlessTransport(tmuxPath string) headlessTransport {
	if tmuxPath != "" {
		return transportTmux
	}
	return transportPTY
}

// buildTmuxArgs builds the `tmux …` argv that starts a detached session running
// `binary args…` with env applied via an `env K=V…` wrapper (argv form, no
// shell). `-P -F '#{pane_pid}'` makes tmux print the pane command's PID.
func buildTmuxArgs(session string, env []string, binary string, args []string) []string {
	out := []string{"new-session", "-d", "-P", "-F", "#{pane_pid}", "-s", session, "env"}
	out = append(out, env...)
	out = append(out, binary)
	out = append(out, args...)
	return out
}

// parsePanePID parses the `#{pane_pid}` value tmux prints to stdout.
func parsePanePID(out string) (int, error) {
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse pane pid %q: %w", out, err)
	}
	return pid, nil
}
