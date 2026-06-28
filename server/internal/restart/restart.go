// Package restart performs a graceful, web-triggered server restart: either an
// in-place re-exec of the current binary (default) or a clean exit for an
// external supervisor to relaunch.
package restart

import (
	"log/slog"
	"os"
	"syscall"
)

type Mode string

const (
	ModeReexec Mode = "reexec"
	ModeExit   Mode = "exit"
)

// Restarter performs the actual relaunch. Seam so the run-loop is testable
// without replacing the process.
type Restarter interface {
	Reexec() error
	Exit()
}

// OSRestarter is the production Restarter.
type OSRestarter struct{}

// Reexec replaces the current process image with a fresh run of the same binary,
// preserving args + environment (same PID). It only returns on error.
func (OSRestarter) Reexec() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// Exit terminates the process cleanly so a supervisor restarts it.
func (OSRestarter) Exit() { os.Exit(0) }

// Execute relaunches per mode. For reexec, a failure is fatal (log + exit 1) so
// the process never hangs in a half-down state.
func Execute(mode Mode, r Restarter) {
	if mode == ModeExit {
		r.Exit()
		return
	}
	if err := r.Reexec(); err != nil {
		slog.Error("restart: re-exec failed", "err", err)
		os.Exit(1)
	}
}

// Controller carries the restart signal from the HTTP handler to the run-loop
// and records the configured mode.
type Controller struct {
	ch   chan struct{}
	mode Mode
}

func NewController(mode string) *Controller {
	m := Mode(mode)
	if m != ModeExit {
		m = ModeReexec
	}
	return &Controller{ch: make(chan struct{}, 1), mode: m}
}

// Trigger requests a restart; non-blocking and coalescing (buffered size 1).
func (c *Controller) Trigger() {
	select {
	case c.ch <- struct{}{}:
	default:
	}
}

// C is the receive end the run-loop selects on.
func (c *Controller) C() <-chan struct{} { return c.ch }

// Mode is the configured relaunch mode (for the 202 response + Execute).
func (c *Controller) Mode() Mode { return c.mode }
