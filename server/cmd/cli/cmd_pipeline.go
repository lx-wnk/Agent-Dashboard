package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPipelineCmd(cfg *CLIConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Pipeline control commands",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show pipeline config and runner slot status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl := newClient(*cfg)
			var config map[string]any
			if err := cl.get("/api/pipeline/config", &config); err != nil {
				return err
			}
			return printJSON(config)
		},
	}

	configGetCmd := &cobra.Command{
		Use:   "config get <key>",
		Short: "Read a pipeline config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl := newClient(*cfg)
			var result map[string]any
			if err := cl.get("/api/pipeline/config", &result); err != nil {
				return err
			}
			if v, ok := result[args[0]]; ok {
				fmt.Println(v)
				return nil
			}
			return fmt.Errorf("key %q not found in pipeline config", args[0])
		},
	}

	configSetCmd := &cobra.Command{
		Use:   "config set <key> <value>",
		Short: "Update a pipeline config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl := newClient(*cfg)
			body := map[string]any{"key": args[0], "value": args[1]}
			return cl.post("/api/pipeline/config", body, nil)
		},
	}

	configCmd := &cobra.Command{Use: "config", Short: "Pipeline config management"}
	configCmd.AddCommand(configGetCmd, configSetCmd)
	cmd.AddCommand(statusCmd, configCmd)
	return cmd
}
