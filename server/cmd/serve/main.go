package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/lx-wnk/agent-dashboard/server/internal/agentbroadcast"
	"github.com/lx-wnk/agent-dashboard/server/internal/channel"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	var cfgFile string

	root := &cobra.Command{
		Use:   "agent-dashboard",
		Short: "Claude Code agent monitoring dashboard",
	}

	serve := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			srv, broadcaster, orch, cleanup, err := initializeServer(ctx, cfg, cfgFile)
			if err != nil {
				return err
			}
			defer cleanup()

			g, ctx := errgroup.WithContext(ctx)

			interval := time.Duration(cfg.SSEIntervalMs) * time.Millisecond
			g.Go(func() error {
				agentbroadcast.Run(ctx, broadcaster, interval)
				return nil
			})

			g.Go(func() error {
				return srv.Run(ctx)
			})

			g.Go(func() error {
				return orch.Run(ctx)
			})

			return g.Wait()
		},
	}
	serve.Flags().StringVar(&cfgFile, "config", "", "path to JSON config file")

	// channel subcommand: runs the dashboard-channel MCP stdio server.
	// Claude Code spawns this when it reads the --mcp-config written by the pipeline spawner.
	channelCmd := &cobra.Command{
		Use:   "channel",
		Short: "Run the dashboard-channel MCP stdio server (invoked by Claude Code)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Redirect slog to stderr: the channel bridge uses os.Stdin/os.Stdout
			// for MCP stdio transport — any log lines on stdout corrupt the protocol.
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			})))
			return channel.Run(cmd.Context())
		},
		// Hide from help output — this is an internal subcommand for Claude Code.
		Hidden: true,
	}
	root.AddCommand(channelCmd)

	root.AddCommand(serve)

	if err := root.Execute(); err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
}
