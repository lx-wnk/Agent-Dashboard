package agents

import (
	"reflect"
	"testing"
)

func TestSelectHeadlessTransport(t *testing.T) {
	if got := selectHeadlessTransport("/usr/bin/tmux"); got != transportTmux {
		t.Errorf("tmux present → %v, want transportTmux", got)
	}
	if got := selectHeadlessTransport(""); got != transportPTY {
		t.Errorf("no tmux → %v, want transportPTY", got)
	}
}

func TestBuildTmuxArgs(t *testing.T) {
	got := buildTmuxArgs("claude-spawn-x", []string{"FOO=bar", "BAZ=qux"}, "claude", []string{"--model", "opus", "hi there"})
	want := []string{
		"new-session", "-d", "-P", "-F", "#{pane_pid}", "-s", "claude-spawn-x",
		"env", "FOO=bar", "BAZ=qux", "claude", "--model", "opus", "hi there",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestParsePanePID(t *testing.T) {
	pid, err := parsePanePID("48213\n")
	if err != nil || pid != 48213 {
		t.Fatalf("pid=%d err=%v", pid, err)
	}
	if _, err := parsePanePID("not-a-pid"); err == nil {
		t.Error("expected error on non-numeric output")
	}
}
