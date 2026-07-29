// server/internal/api/server.go
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
)

const ShutdownTimeout = 10 * time.Second

// Server wraps net/http.Server with graceful shutdown support.
type Server struct {
	httpSrv         *http.Server
	shutdownTimeout time.Duration
}

// NewServer creates a Server bound to addr serving handler.
// If timeout is <= 0, ShutdownTimeout is used as fallback.
func NewServer(addr string, handler http.Handler, timeout time.Duration) *Server {
	if timeout <= 0 {
		timeout = ShutdownTimeout
	}
	return &Server{
		httpSrv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second, // prevent Slowloris
		},
		shutdownTimeout: timeout,
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled or an error occurs.
// On ctx cancellation, performs graceful shutdown within s.shutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	// Shutdown does not cancel in-flight request contexts, so streaming handlers
	// derive theirs from BaseContext to observe cancellation instead of blocking Shutdown.
	runCtx := ctx
	s.httpSrv.BaseContext = func(net.Listener) context.Context { return runCtx }

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		slog.Info("server starting", "addr", s.httpSrv.Addr)
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done() // wait for shutdown signal
		slog.Info("server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	})

	return g.Wait()
}
