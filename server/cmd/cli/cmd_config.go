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
		Use:   "set",
		Short: "Update CLI configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if u, _ := cmd.Flags().GetString("url"); u != "" {
				cfg.Host = u
			}
			if t, _ := cmd.Flags().GetString("token"); t != "" {
				cfg.Token = t
			}
			if err := saveConfig(*cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			return nil
		},
	}
	setCmd.Flags().String("url", "", "Dashboard server URL")
	setCmd.Flags().String("token", "", "API bearer token")

	cmd.AddCommand(showCmd, setCmd)
	return cmd
}
