package main

import (
	"testing"
)

func TestSelectTransport_InsideTmux(t *testing.T) {
	// When $TMUX is set, we are already inside a tmux session.
	got := selectTransport("some-tmux-value", "/usr/bin/tmux")
	if got != transportInsideTmux {
		t.Errorf("expected transportInsideTmux, got %v", got)
	}
}

func TestSelectTransport_InsideTmux_NoTmuxOnPath(t *testing.T) {
	// Even if tmux is not on PATH, being inside tmux takes priority.
	got := selectTransport("some-tmux-value", "")
	if got != transportInsideTmux {
		t.Errorf("expected transportInsideTmux when TMUX set and no tmux on PATH, got %v", got)
	}
}

func TestSelectTransport_NewTmux(t *testing.T) {
	// $TMUX is empty but tmux is available on PATH — create a new session.
	got := selectTransport("", "/usr/bin/tmux")
	if got != transportNewTmux {
		t.Errorf("expected transportNewTmux, got %v", got)
	}
}

func TestSelectTransport_PTY_NoTmux(t *testing.T) {
	// Neither inside tmux nor tmux on PATH — fall back to the pty broker.
	got := selectTransport("", "")
	if got != transportPTY {
		t.Errorf("expected transportPTY, got %v", got)
	}
}
