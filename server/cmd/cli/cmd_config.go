package main

import (
	"fmt"
	"strings"

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
			type redactedConfig struct {
				Host  string `json:"host"`
				Token string `json:"token"`
			}
			rc := redactedConfig{Host: cfg.Host}
			if cfg.Token != "" {
				if len(cfg.Token) > 8 {
					rc.Token = cfg.Token[:4] + strings.Repeat("*", len(cfg.Token)-8) + cfg.Token[len(cfg.Token)-4:]
				} else {
					rc.Token = "****"
				}
			}
			return printJSON(rc)
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
