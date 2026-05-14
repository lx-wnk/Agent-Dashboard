package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newPipelineCmd(cfg *CLIConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Pipeline control commands",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show pipeline runner recommendation",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl := newClient(*cfg)
			var recommendation map[string]any
			if err := cl.get("/api/pipeline/recommendation", &recommendation); err != nil {
				return err
			}
			return printJSON(recommendation)
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
			parsedInt, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("value must be an integer: %w", err)
			}
			cl := newClient(*cfg)
			body := map[string]any{args[0]: parsedInt}
			var result map[string]any
			return cl.put("/api/pipeline/config", body, &result)
		},
	}

	configCmd := &cobra.Command{Use: "config", Short: "Pipeline config management"}
	configCmd.AddCommand(configGetCmd, configSetCmd)
	cmd.AddCommand(statusCmd, configCmd)
	return cmd
}
