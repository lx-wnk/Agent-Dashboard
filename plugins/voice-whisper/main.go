package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	srv := NewServer(newWhisperCLI())
	addr := "127.0.0.1:19010"
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	slog.Info("voice-whisper listening", "addr", addr)
	if err := httpSrv.ListenAndServe(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
