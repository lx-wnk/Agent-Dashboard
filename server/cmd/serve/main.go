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

			if cfg.Host != "127.0.0.1" && cfg.Host != "::1" && cfg.Host != "localhost" {
				slog.Warn("server binding to non-loopback address — ensure this is intentional",
					"host", cfg.Host)
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			srv, broadcaster, err := initializeServer(cfg)
			if err != nil {
				return err
			}

			g, ctx := errgroup.WithContext(ctx)

			interval := time.Duration(cfg.SSEIntervalMs) * time.Millisecond
			g.Go(func() error {
				agentbroadcast.Run(ctx, broadcaster, interval)
				return nil
			})

			g.Go(func() error {
				return srv.Run(ctx)
			})

			return g.Wait()
		},
	}
	serve.Flags().StringVar(&cfgFile, "config", "", "path to JSON config file")

	root.AddCommand(serve)

	if err := root.Execute(); err != nil {
		slog.Error("startup failed", "err", err)
		os.Exit(1)
	}
}
