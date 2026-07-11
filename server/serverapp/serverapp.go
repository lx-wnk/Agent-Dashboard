// Package serverapp wires and runs the dashboard HTTP server. It is imported
// both by the cmd/serve CLI (out-of-process) and, in future, by a desktop
// shell module that starts the server in-process as a goroutine — hence it
// lives outside internal/ so it is importable across module boundaries.
package serverapp

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/lx-wnk/agent-dashboard/server/internal/agentbroadcast"
	"github.com/lx-wnk/agent-dashboard/server/internal/config"
	"github.com/lx-wnk/agent-dashboard/server/internal/restart"
)

// Run wires the server via initializeServer and blocks until ctx is
// cancelled or a fatal error occurs in any of the run-loop members. It
// returns after graceful shutdown and cleanup.
//
// The caller owns ctx cancellation (the CLI derives it from OS signals; an
// in-process host such as a desktop shell derives it from window-close).
// Restart is handled internally: on a restart trigger, Run cancels its own
// derived context (not the caller's ctx) so cleanup and restart.Execute run
// the same way whether the process is asked to restart or the caller simply
// cancels ctx.
func Run(ctx context.Context, cfg config.Config, cfgFile string, restartCtl *restart.Controller) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	srv, broadcaster, agentMerger, orch, sched, histImporter, baselineProvider, enricher, evalService, settingsSvc, cleanup, err := initializeServer(runCtx, cfg, cfgFile, restartCtl)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return err
	}

	g, gCtx := errgroup.WithContext(runCtx)

	interval := time.Duration(settingsSvc.Int("sse.intervalMs")) * time.Millisecond
	g.Go(func() error {
		agentbroadcast.Run(gCtx, agentMerger, broadcaster, interval, baselineProvider, enricher)
		return nil
	})

	g.Go(func() error {
		return srv.Run(gCtx)
	})

	g.Go(func() error {
		return orch.Run(gCtx)
	})

	if sched != nil {
		g.Go(func() error {
			return sched.Run(gCtx)
		})
	}

	if histImporter != nil {
		g.Go(func() error {
			histImporter.RunScheduled(gCtx, time.Duration(settingsSvc.Int("cost.scanIntervalMs"))*time.Millisecond)
			return nil
		})
	}

	if evalService != nil {
		g.Go(func() error {
			evalService.RunLoop(gCtx, time.Duration(settingsSvc.Int("eval.scanIntervalMs"))*time.Millisecond)
			return nil
		})
	}

	restarting := false
	g.Go(func() error {
		select {
		case <-gCtx.Done():
			return nil
		case <-restartCtl.C():
			restarting = true
			cancel() // cancel our own derived context → graceful shutdown of all g.Go members
			return nil
		}
	})

	err = g.Wait()
	cleanup() // stop plugins etc. BEFORE any re-exec (deferred funcs won't run after Exec)
	if restarting {
		slog.Info("restart: relaunching", "mode", restartCtl.Mode())
		restart.Execute(restartCtl.Mode(), restart.OSRestarter{})
	}
	return err
}

// Serve loads configuration from cfgFile (empty for defaults), builds the
// restart controller, and runs the server until ctx is cancelled. It is the
// entrypoint for callers outside the server module (e.g. the desktop shell),
// which cannot construct the internal config/restart types themselves.
func Serve(ctx context.Context, cfgFile string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	restartCtl := restart.NewController(cfg.RestartMode)
	return Run(ctx, cfg, cfgFile, restartCtl)
}
