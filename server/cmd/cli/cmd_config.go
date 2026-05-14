package main

import (
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
			token := "****"
			if cfg.Token == "" {
				token = "(not set)"
			}
			rc.Token = token
			return printJSON(rc)
		},
	}

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Update CLI configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			fresh, err := loadConfig()
			if err != nil {
				fresh = CLIConfig{}
			}
			if u, _ := cmd.Flags().GetString("url"); u != "" {
				fresh.Host = u
			}
			if t, _ := cmd.Flags().GetString("token"); t != "" {
				fresh.Token = t
			}
			return saveConfig(fresh)
		},
	}
	setCmd.Flags().String("url", "", "Dashboard server URL")
	setCmd.Flags().String("token", "", "API bearer token")

	cmd.AddCommand(showCmd, setCmd)
	return cmd
}
