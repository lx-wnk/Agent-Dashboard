package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := envOr("VOICE_WEBSPEECH_ADDR", "127.0.0.1:19011")
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           NewServer(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	slog.Info("voice-webspeech listening", "addr", addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
