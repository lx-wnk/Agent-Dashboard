package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	srv := NewServer(newWhisperCLI())
	addr := envOr("VOICE_WHISPER_ADDR", "127.0.0.1:19010")
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadTimeout:       120 * time.Second, // covers full audio upload body; ReadHeaderTimeout guards slowloris
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	slog.Info("voice-whisper listening", "addr", addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}
