package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %s\n", err)
		os.Exit(1)
	}

	root := &cobra.Command{
		Use:   "dashboard",
		Short: "Claude Agent Dashboard CLI",
		Long: `dashboard is a CLI client for the Claude Agent Dashboard server.

Configure it with:
  dashboard config set host http://127.0.0.1:13120
  dashboard config set token mcp_your_token_here

Create an API token in the dashboard web UI under Settings > API Keys.`,
		SilenceUsage: true,
	}

	root.AddCommand(
		newAgentsCmd(&cfg),
		newTasksCmd(&cfg),
		newPipelineCmd(&cfg),
		newConfigCmd(&cfg),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
