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

// RequestGraceDefault is how long in-flight, non-streaming request contexts
// are allowed to keep running after shutdown starts before they are
// cancelled. 2s comfortably covers a DB write, git commit, or worktree
// operation that was mid-flight when quit was triggered, without the delay
// being noticeable on quit. It must stay below shutdownTimeout: Shutdown
// carries its own deadline, and if that fires first the grace window never
// gets a chance to matter.
const RequestGraceDefault = 2 * time.Second

// Server wraps net/http.Server with graceful shutdown support.
type Server struct {
	httpSrv         *http.Server
	shutdownTimeout time.Duration
	requestGrace    time.Duration
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
		requestGrace:    clampRequestGrace(RequestGraceDefault, timeout),
	}
}

// SetRequestGrace overrides the request grace window (clamped below
// shutdownTimeout, same as the default). Exposed for tests that need a short
// grace instead of waiting out production-sized defaults.
func (s *Server) SetRequestGrace(d time.Duration) {
	s.requestGrace = clampRequestGrace(d, s.shutdownTimeout)
}

// clampRequestGrace keeps grace below timeout: shutdownTimeout comes from the
// user-configurable shutdown.timeoutSeconds setting (min 1s), which can be
// set below RequestGraceDefault.
func clampRequestGrace(grace, timeout time.Duration) time.Duration {
	if grace >= timeout {
		return timeout / 2
	}
	return grace
}

// Run starts the HTTP server and blocks until ctx is cancelled or an error occurs.
// On ctx cancellation, performs graceful shutdown within s.shutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	// Shutdown does not cancel in-flight request contexts on its own, so a
	// streaming handler that only selects on r.Context().Done() would block
	// it forever. reqCtx is cancelled separately, s.requestGrace after
	// shutdown starts: long enough for an ordinary in-flight request
	// (DB write, git commit, worktree op) to finish, short enough that
	// streaming handlers still release Shutdown well inside its own
	// deadline. WithoutCancel drops ctx's cancellation from reqCtx so
	// nothing but the AfterFunc below ever cancels it.
	reqCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	s.httpSrv.BaseContext = func(net.Listener) context.Context { return reqCtx }

	g.Go(func() error {
		slog.Info("server starting", "addr", s.httpSrv.Addr)
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		// Reading from the errgroup's own ctx (not the caller's) means a
		// failing listener also releases request contexts, instead of
		// leaving Shutdown to burn its whole timeout on the one path this
		// exists for.
		<-ctx.Done()
		slog.Info("server shutting down")
		grace := time.AfterFunc(s.requestGrace, cancelRequests)
		defer grace.Stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	})

	return g.Wait()
}
