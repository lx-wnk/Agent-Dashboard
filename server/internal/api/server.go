// server/internal/api/server.go
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
)

const ShutdownTimeout = 10 * time.Second

// Server wraps net/http.Server with graceful shutdown support.
type Server struct {
	httpSrv *http.Server
}

// NewServer creates a Server bound to addr serving handler.
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		httpSrv: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second, // prevent Slowloris
		},
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled or an error occurs.
// On ctx cancellation, performs graceful shutdown within ShutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	})

	return g.Wait()
}
