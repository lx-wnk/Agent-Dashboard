package serverapp

import (
	"context"
	"strings"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
)

// TestDispatchHeadless_GuiPathNeverHijacked asserts DispatchHeadless leaves the
// GUI startup path alone for argv shapes that name no headless subcommand,
// including the "-psn_..." argument macOS passes to a Finder-launched .app.
func TestDispatchHeadless_GuiPathNeverHijacked(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"nil args", nil},
		{"empty args", []string{}},
		{"macOS Finder launch arg", []string{"-psn_0_12345"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := DispatchHeadless(context.Background(), tc.args)
			if handled {
				t.Errorf("handled = true, want false for args %#v", tc.args)
			}
			if err != nil {
				t.Errorf("err = %v, want nil for args %#v", err, tc.args)
			}
		})
	}
}

// TestDispatchHeadless_PtyHostRecognised proves the pty-host subcommand name is
// recognised without needing to spawn a real pty child.
func TestDispatchHeadless_PtyHostRecognised(t *testing.T) {
	cases := [][]string{
		{channelconfig.SubcommandPtyHost},
		{channelconfig.SubcommandPtyHost, "--"},
	}

	for _, args := range cases {
		handled, err := DispatchHeadless(context.Background(), args)
		if !handled {
			t.Fatalf("handled = false, want true for args %#v", args)
		}
		if err == nil {
			t.Fatalf("err = nil, want a 'no command given' error for args %#v", args)
		}
		if !strings.Contains(err.Error(), "no command given") {
			t.Errorf("err = %q, want it to mention 'no command given'", err.Error())
		}
	}
}

// TestHeadlessSubcommands_MatchesSpawnerReExecSites guards against a future
// re-exec site (spawn.go, pipeline/spawner.go) that the desktop binary cannot
// serve: every subcommand the spawner re-executes must be listed here.
func TestHeadlessSubcommands_MatchesSpawnerReExecSites(t *testing.T) {
	got := HeadlessSubcommands()

	want := []string{channelconfig.SubcommandPtyHost, channelconfig.SubcommandChannel}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("HeadlessSubcommands() = %v, want it to contain %q", got, w)
		}
	}
}
