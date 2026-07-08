package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"golang.org/x/term"
)

// RunPTY launches command under a pseudo-terminal it owns, proxies the current
// terminal to it (so the user interacts normally, full TUI), and serves a
// loopback HTTP /message endpoint that injects text as real keyboard input into
// the child. This is the tmux-free path for live prompt injection: it works on
// macOS and Linux without any external multiplexer because the broker holds the
// pty master. Blocks until the child exits; returns its error.
func RunPTY(ctx context.Context, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("ptyhost: no command given")
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("ptyhost: start: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	hub := newPtyHub(256 * 1024)

	// Keep the pty sized to the real terminal (initial + on resize).
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}()
	winch <- syscall.SIGWINCH
	defer signal.Stop(winch)

	// Put the real terminal in raw mode so keystrokes pass through untouched.
	if oldState, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
	}

	// HTTP inject endpoint, keyed by the CHILD pid (the claude process the
	// scanner sees). Mirrors the bridge discovery contract so the dashboard's
	// existing /message delivery works — but writes to the pty, not an MCP log.
	childPid := cmd.Process.Pid
	initialToken, err := generateToken()
	if err != nil {
		return fmt.Errorf("ptyhost: token: %w", err)
	}
	token := newRotatingToken(initialToken)
	srv, port, err := startPtyHTTPServer(ptmx, token)
	if err != nil {
		return fmt.Errorf("ptyhost: http: %w", err)
	}
	// Foreground sessions proxy output directly to the user's terminal and are
	// interactive/owned by the user; treat them as always-recent (time.Now())
	// rather than tracking output, which the dashboard interprets as live.
	discPath, derr := writePtyDiscovery(childPid, port, token.value(), time.Now())
	if derr != nil {
		slog.Warn("ptyhost: discovery write failed", "err", derr)
	}

	// Rotate the inject token periodically, re-emitting the 0600 discovery file.
	go startTokenRotation(ctx, token, injectTokenRotateInterval(), func(newToken string) error {
		_, werr := writePtyDiscovery(childPid, port, newToken, time.Now())
		return werr
	})
	defer func() {
		if discPath != "" {
			_ = os.Remove(discPath)
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	// Proxy: terminal stdin → child, child output → terminal.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	_, _ = io.Copy(io.MultiWriter(os.Stdout, hub), ptmx) // returns when the child exits / pty closes

	return cmd.Wait()
}

// startPtyHTTPServer serves POST /message: the body's `message` is written to
// the pty followed by a carriage return, i.e. injected as if typed + Enter.
func startPtyHTTPServer(ptmx io.Writer, token *rotatingToken) (*http.Server, int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
	mux.HandleFunc("POST /message", func(w http.ResponseWriter, r *http.Request) {
		if !token.authorize(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Message == "" {
			http.Error(w, `{"error":"missing message"}`, http.StatusBadRequest)
			return
		}
		// Inject the text, then a carriage return to submit it. CR (\r) is what a
		// real Enter sends on a pty.
		if _, err := io.WriteString(ptmx, payload.Message+"\r"); err != nil {
			http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /keys", func(w http.ResponseWriter, r *http.Request) {
		if !token.authorize(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		var batches [][]answerKeyWire
		if err := json.Unmarshal(body, &batches); err != nil {
			http.Error(w, `{"error":"invalid batches"}`, http.StatusBadRequest)
			return
		}
		for bi, batch := range batches {
			for _, k := range batch {
				if _, err := io.WriteString(ptmx, answerKeyBytes(k)); err != nil {
					http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
					return
				}
			}
			if bi < len(batches)-1 {
				answerKeyStepSleep()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return srv, port, nil
}

// answerKeyWire mirrors the JSON shape the agents package encodes AnswerKey
// batches as for the /keys endpoint. It is a local, independent copy — the
// channel package must not import the agents package.
type answerKeyWire struct {
	Char  string `json:"char"`
	Named string `json:"named"`
	Text  string `json:"text"`
}

// answerKeyBytes returns the raw bytes to write to the pty for a single
// interactive-question keystroke. Unlike /message, no trailing \r is added —
// the caller's batch already includes an explicit Enter where one is needed.
func answerKeyBytes(k answerKeyWire) string {
	switch {
	case k.Text != "":
		return k.Text
	case k.Char != "":
		return k.Char
	}
	switch k.Named {
	case "Enter":
		return "\r"
	case "Tab":
		return "\t"
	case "Space":
		return " "
	case "Up":
		return "\x1b[A"
	case "Down":
		return "\x1b[B"
	default:
		return ""
	}
}

// answerKeyStepDelay is the pause between key batches so the next selector
// renders before its keys are sent. Indirected via answerKeyStepSleep for tests.
var answerKeyStepDelay = 250 * time.Millisecond

var answerKeyStepSleep = func() { time.Sleep(answerKeyStepDelay) }

// writePtyDiscovery writes the pty-broker discovery file for a pty-hosted
// session. It writes to {childPid}.pty.json (not {childPid}.json) so it never
// collides with the channel bridge's {parentPid}.json file when both run for
// the same claude process. The dashboard reads BOTH files independently:
//   - {pid}.json  → channel bridge (channelAvailable, tmuxPane)
//   - {pid}.pty.json → pty broker (channelAvailable, ptyInject)
func writePtyDiscovery(childPid, port int, token string, lastOutputAt time.Time) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := channelconfig.DiscoveryPtyFile(home, childPid)
	data, _ := json.Marshal(map[string]any{
		"port":         port,
		"token":        token,
		"parentPid":    childPid,
		"cwd":          cwd(),
		"ptyInject":    true,
		"startedAt":    time.Now().UTC().Format(time.RFC3339),
		"lastOutputAt": lastOutputAt.UTC().Format(time.RFC3339),
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
