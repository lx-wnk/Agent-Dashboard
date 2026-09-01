package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// resolveDBPath: --db flag > DASHBOARD_DB_PATH > default ~/.claude/dashboard-tasks.db
func resolveDBPath(cmd *cobra.Command) (string, error) {
	if p, _ := cmd.Flags().GetString("db"); p != "" {
		return p, nil
	}
	if p := os.Getenv("DASHBOARD_DB_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.claude/dashboard-tasks.db", nil
}

func withStore(cmd *cobra.Command, fn func(ctx context.Context, s *dbStore) error) error {
	path, err := resolveDBPath(cmd)
	if err != nil {
		return err
	}
	store, err := openDBStore(path)
	if err != nil {
		return fmt.Errorf("open db %s: %w", path, err)
	}
	defer func() { _ = store.Close() }()
	return fn(cmd.Context(), store)
}

func newSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "settings", Short: "Read/write DB-backed server settings (direct DB access)"}
	cmd.PersistentFlags().String("db", "", "Path to the dashboard SQLite DB (default: $DASHBOARD_DB_PATH or ~/.claude/dashboard-tasks.db)")

	list := &cobra.Command{Use: "list", Short: "List all settings (effective values)", RunE: func(cmd *cobra.Command, _ []string) error {
		return withStore(cmd, func(ctx context.Context, s *dbStore) error {
			rows, err := s.List(ctx)
			if err != nil {
				return err
			}
			for _, d := range settings.All() {
				val := d.Default
				if v, ok := rows[d.Key]; ok {
					val = v
				}
				fmt.Printf("%-28s = %-12s (%s, %s)\n", d.Key, val, d.Type, d.Apply)
			}
			return nil
		})
	}}

	get := &cobra.Command{Use: "get <key>", Short: "Get a setting value", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(ctx context.Context, s *dbStore) error {
			v, ok, err := s.Get(ctx, args[0])
			if err != nil {
				return err
			}
			if !ok {
				if d, found := settings.Lookup(args[0]); found {
					fmt.Println(d.Default)
					return nil
				}
				return errUnknownKey(args[0])
			}
			fmt.Println(v)
			return nil
		})
	}}

	set := &cobra.Command{Use: "set <key> <value>", Short: "Set a setting value", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withStore(cmd, func(ctx context.Context, s *dbStore) error {
			if err := s.SetValidated(ctx, args[0], args[1]); err != nil {
				return err
			}
			// Never echo a secret value back, even the one just typed: it
			// would land in terminal scrollback, tmux capture-pane, or a
			// redirected log file.
			display := args[1]
			if d, ok := settings.Lookup(args[0]); ok && d.Secret {
				display = secretbox.MaskedSentinel
			}
			fmt.Printf("set %s = %s\n", args[0], display)
			return nil
		})
	}}

	cmd.AddCommand(list, get, set)
	return cmd
}
