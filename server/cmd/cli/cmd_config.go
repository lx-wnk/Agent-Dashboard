package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newConfigCmd(cfg *CLIConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "CLI configuration",
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Print current CLI config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printJSON(cfg)
		},
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value (host or token)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "host":
				cfg.Host = args[1]
			case "token":
				cfg.Token = args[1]
			default:
				return fmt.Errorf("unknown config key %q — supported: host, token", args[0])
			}
			if err := saveConfig(*cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("Set %s = %s\n", args[0], args[1])
			return nil
		},
	}

	cmd.AddCommand(showCmd, setCmd)
	return cmd
}
