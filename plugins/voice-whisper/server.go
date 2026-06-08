package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"os"
)

//go:embed addon.js
var addonJS []byte

// Transcriber turns an audio file on disk into text. The real implementation
// (whisper.go) shells out to ffmpeg + whisper.cpp; tests inject a fake.
type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

func NewServer(t Transcriber) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/addon.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write(addonJS)
	})

	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		file, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "missing audio field", http.StatusBadRequest)
			return
		}
		defer file.Close()

		tmp, err := os.CreateTemp("", "voice-*.webm")
		if err != nil {
			http.Error(w, "temp file", http.StatusInternalServerError)
			return
		}
		defer os.Remove(tmp.Name())
		if _, err := io.Copy(tmp, file); err != nil {
			http.Error(w, "write audio", http.StatusInternalServerError)
			return
		}
		tmp.Close()

		text, err := t.Transcribe(r.Context(), tmp.Name())
		if err != nil {
			http.Error(w, "transcription failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": text})
	})

	return mux
}
