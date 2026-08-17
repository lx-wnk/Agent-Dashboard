package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// whisperCLI converts webm/opus → wav via ffmpeg, then runs whisper.cpp and reads
// the produced .txt. Binary + model paths come from env (see plugin.json env list).
type whisperCLI struct {
	ffmpegBin  string // default "ffmpeg"
	whisperBin string // VOICE_WHISPER_BIN
	modelPath  string // VOICE_WHISPER_MODEL
	lang       string // VOICE_WHISPER_LANG; "auto" lets whisper detect (needs a multilingual model)
}

func newWhisperCLI() whisperCLI {
	return whisperCLI{
		ffmpegBin:  envOr("FFMPEG_BIN", "ffmpeg"),
		whisperBin: envOr("VOICE_WHISPER_BIN", "whisper-cli"),
		modelPath:  envOr("VOICE_WHISPER_MODEL", "models/ggml-base.en.bin"),
		lang:       envOr("VOICE_WHISPER_LANG", "auto"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c whisperCLI) Transcribe(ctx context.Context, audioPath string) (string, error) {
	wav := audioPath + ".wav"
	defer func() { _ = os.Remove(wav) }()
	// -ar 16000 -ac 1: whisper.cpp expects 16kHz mono.
	// #nosec G204 -- ffmpegBin comes from the server process environment via the plugin descriptor allow-list (buildPluginEnv), never from DB settings or a request; audioPath is an os.CreateTemp path, not the upload filename.
	conv := exec.CommandContext(ctx, c.ffmpegBin, "-y", "-i", audioPath,
		"-ar", "16000", "-ac", "1", wav)
	if out, err := conv.CombinedOutput(); err != nil {
		return "", &cmdErr{"ffmpeg", out, err}
	}

	outBase := audioPath + ".out"
	defer func() { _ = os.Remove(outBase + ".txt") }()
	// whisper.cpp: -otxt writes <outBase>.txt; -l auto detects language per clip
	// (requires a multilingual model — the *.en models are English-only).
	// #nosec G204 -- whisperBin, modelPath and lang all come from the server process environment via the plugin descriptor allow-list (buildPluginEnv); no request or DB setting can reach them.
	w := exec.CommandContext(ctx, c.whisperBin, "-m", c.modelPath, "-f", wav,
		"-l", c.lang, "-otxt", "-of", outBase, "-nt")
	if out, err := w.CombinedOutput(); err != nil {
		return "", &cmdErr{"whisper", out, err}
	}
	txt, err := os.ReadFile(outBase + ".txt")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(txt)), nil
}

type cmdErr struct {
	stage string
	out   []byte
	err   error
}

func (e *cmdErr) Error() string {
	return e.stage + ": " + e.err.Error() + ": " + string(e.out)
}
