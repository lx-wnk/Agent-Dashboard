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
  dashboard config set --token <jwt>

Authentication: the token must be a JWT issued by the dashboard server.
The CLI works when the server runs in loopback bypass mode (no GitHub OAuth
configured). MCP API keys (mcp_<hex>) do NOT work with this CLI — they only
authenticate against POST /api/mcp.`,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&flagURL, "url", "", "Dashboard server URL (overrides config)")
	root.PersistentFlags().StringVar(&flagToken, "token", "", "API token (overrides config)")

	// cfg is loaded lazily via PersistentPreRunE so flag overrides apply before subcommands run.
	var cfg CLIConfig
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		loaded, err := loadConfig()
		if err != nil {
			return fmt.Errorf("config error: %w", err)
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
		newSettingsCmd(),
		newPluginsCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
