package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	var flagURL, flagToken string

	root := &cobra.Command{
		Use:   "dashboard",
		Short: "Claude Agent Dashboard CLI",
		Long: `dashboard is a CLI client for the Claude Agent Dashboard server.

Configure it with:
  dashboard config set --url http://127.0.0.1:13120
  dashboard config set --token mcp_your_token_here

Create an API token in the dashboard web UI under Settings > API Keys.`,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&flagURL, "url", "", "Dashboard server URL (overrides config)")
	root.PersistentFlags().StringVar(&flagToken, "token", "", "API token (overrides config)")

	// cfg is loaded lazily via PersistentPreRunE so flag overrides apply before subcommands run.
	var cfg CLIConfig
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		loaded, err := loadConfig()
		if err != nil {
			return fmt.Errorf("config error: %s", err)
		}
		cfg = loaded
		if root.PersistentFlags().Changed("url") {
			cfg.Host = flagURL
		}
		if root.PersistentFlags().Changed("token") {
			cfg.Token = flagToken
		}
		return nil
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
