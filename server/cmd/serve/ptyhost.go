package main

import (
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/channel"
	"github.com/spf13/cobra"
)

// newPtyHostCmd builds `agent-dashboard pty-host -- <binary> <args…>`. It runs
// the command on a dashboard-owned pty (headless), serving live injection, and
// prints the child PID as the first stdout line so the spawner can capture it.
func newPtyHostCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "pty-host [-- command args...]",
		Short:              "Run a command on a dashboard-owned pty for headless live injection",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			if len(args) == 0 {
				return fmt.Errorf("pty-host: no command given")
			}
			return channel.RunHeadlessPTY(cmd.Context(), args[0], args[1:], nil, "", func(pid int) {
				fmt.Println(pid)
			})
		},
	}
}
