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

// version is the dashboard version. It defaults to "dev" for local builds and is
// overridden at release time via -ldflags "-X main.version=<tag>" (see .goreleaser.yml).
var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	var cfgFile string

	root := &cobra.Command{
		Use:     "agent-dashboard",
		Short:   "Claude Code agent monitoring dashboard",
		Version: version,
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

			srv, broadcaster, orch, sched, histImporter, baselineProvider, enricher, cleanup, err := initializeServer(ctx, cfg, cfgFile)
			if err != nil {
				return err
			}
			defer cleanup()

			g, ctx := errgroup.WithContext(ctx)

			interval := time.Duration(cfg.SSEIntervalMs) * time.Millisecond
			g.Go(func() error {
				agentbroadcast.Run(ctx, broadcaster, interval, baselineProvider, enricher)
				return nil
			})

			g.Go(func() error {
				return srv.Run(ctx)
			})

			g.Go(func() error {
				return orch.Run(ctx)
			})

			if sched != nil {
				g.Go(func() error {
					return sched.Run(ctx)
				})
			}

			if histImporter != nil {
				g.Go(func() error {
					histImporter.RunScheduled(ctx, time.Duration(cfg.CostScanIntervalMs)*time.Millisecond)
					return nil
				})
			}

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

	ptyhostCmd := &cobra.Command{
		Use:   "ptyhost -- <command> [args...]",
		Short: "Run a command under a pty with dashboard live prompt injection (tmux-free)",
		// Pass every argument through to the child unchanged (e.g. claude's own flags).
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// ptyhost proxies the child via os.Stdout; keep logs off stdout.
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelWarn,
			})))
			// Drop a leading "--" separator if cobra passed it through.
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			return channel.RunPTY(cmd.Context(), args)
		},
		Hidden: true,
	}
	root.AddCommand(ptyhostCmd)

	root.AddCommand(newLiveCmd())

	root.AddCommand(serve)

	if err := root.Execute(); err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
}
