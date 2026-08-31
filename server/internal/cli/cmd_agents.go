package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAgentsCmd(cfg *CLIConfig) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Agent monitoring commands",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all running agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl := newClient(*cfg)
			var agents []map[string]any
			if err := cl.get("/api/agents", &agents); err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(agents)
			}
			tw := newTabWriter()
			fmt.Fprintln(tw, "ID\tSTATUS\tMODEL\tCOST\tWORKDIR")
			for _, a := range agents {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					strField(a, "id"),
					strField(a, "status"),
					strField(a, "model"),
					fmt.Sprintf("$%.4f", floatField(a, "totalCost")),
					strField(a, "workDir"),
				)
			}
			return tw.Flush()
		},
	}
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output raw JSON")

	inspectCmd := &cobra.Command{
		Use:   "inspect <id>",
		Short: "Show full details for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl := newClient(*cfg)
			var agents []map[string]any
			if err := cl.get("/api/agents", &agents); err != nil {
				return err
			}
			for _, a := range agents {
				if strField(a, "id") == args[0] || strField(a, "sessionId") == args[0] {
					return printJSON(a)
				}
			}
			return fmt.Errorf("agent %q not found", args[0])
		},
	}

	cmd.AddCommand(listCmd, inspectCmd)
	return cmd
}

// strField safely reads a string field from a map.
func strField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return "-"
}

// floatField safely reads a float64 field from a map.
func floatField(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}
