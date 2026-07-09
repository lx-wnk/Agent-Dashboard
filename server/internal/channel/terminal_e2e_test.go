package channel

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// TestTerminalE2E_QuestionAnsweredOverBroker is the capstone transport test:
// it proves the full byte path a real interactive question travels — child
// process → real pty → ptyHub → broker GET /ws (real coder/websocket) → WS
// client, and the reverse path for input — client keystroke → broker → pty →
// child stdin → child reacts. It does NOT drive a real `claude` session; see
// the MANUAL note below for the one path this suite cannot cover.
//
// The "agent" is a scripted child (this same test binary, re-exec'd via the
// os.Args[0] + env-var trick — see TestMain/scriptedChildMain) that prints a
// canned AskUserQuestion-shaped modal, waits for one raw byte on stdin, then
// prints a distinct "proceeded" marker. This exercises a genuine *os.File
// pty (github.com/creack/pty), the real ptyMux handler, and a real WebSocket
// client — nothing here is faked.
//
// MANUAL smoke test (not covered by any automated test): spawn a real
// dashboard session with a prompt engineered to trigger a live
// AskUserQuestion, open its Terminal tab in the browser, confirm the overlay
// renders over the streamed pty output, and confirm answering it (via the
// UI, which encodes through src/utils/answerKeys.ts) actually advances the
// underlying claude session. This is the only leg of the transport (real
// claude CLI TUI rendering + real model-driven question timing) that cannot
// be made deterministic in CI — it needs the claude CLI, API auth, and
// folder-trust, plus non-deterministic model latency. Browser-side question
// detection is unit-tested (detectQuestion, Task 10) and the keystroke
// encoding is parity-tested against the real TUI key model (Task 11); this
// file is what proves the transport those two meet on actually works
// end-to-end for a live child process.
const scriptedChildEnv = "TERMINAL_E2E_SCRIPTED_CHILD"

const scriptedModal = "" +
	"+-- Pick a colour --------------------------+\r\n" +
	"| What is your favourite colour?            |\r\n" +
	"|                                            |\r\n" +
	"| > 1. Red                                   |\r\n" +
	"|   2. Green                                 |\r\n" +
	"|   3. Blue                                  |\r\n" +
	"|   4. Type something                        |\r\n" +
	"|   5. Chat about this                       |\r\n" +
	"+--------------------------------------------+\r\n"

const scriptedProceededMarker = "PROCEEDED:answer-received\r\n"

// TestMain re-execs this test binary as a scripted "agent" child when the
// TERMINAL_E2E_SCRIPTED_CHILD env var is set, bypassing the testing package
// entirely so no test-framework output (PASS/ok lines) pollutes the pty
// stream the parent test asserts against.
func TestMain(m *testing.M) {
	if os.Getenv(scriptedChildEnv) == "1" {
		scriptedChildMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// scriptedChildMain stands in for a real agent CLI driving an interactive
// question: it renders a modal, blocks for one raw keystroke, then reacts.
// It puts its own stdin (the pty slave, inherited from the parent) into raw
// mode — matching how real TUI agents read single keystrokes without waiting
// for a newline (see lesson_askuserquestion_tui_keys) — so a single-byte
// answer like "1" is readable immediately instead of buffering in the tty
// line discipline until a newline arrives.
func scriptedChildMain() {
	if oldState, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
	}

	_, _ = os.Stdout.WriteString(scriptedModal)

	buf := make([]byte, 1)
	if _, err := os.Stdin.Read(buf); err != nil {
		return
	}

	_, _ = os.Stdout.WriteString(scriptedProceededMarker)
	// Give the pty a moment to flush the write to the master side before the
	// process (and the pty) tears down.
	time.Sleep(100 * time.Millisecond)
}

// accumulator collects WS frames into a growing buffer and lets the test
// block until a substring shows up, without assuming any particular frame
// boundary — pty output can arrive in arbitrarily small or large chunks.
type accumulator struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (a *accumulator) append(p []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.buf.Write(p)
}

func (a *accumulator) contains(substr string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Contains(a.buf.String(), substr)
}

func (a *accumulator) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.buf.String()
}

// waitForSubstring polls acc until substr shows up or the timeout elapses.
func waitForSubstring(t *testing.T, acc *accumulator, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if acc.contains(substr) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out after %s waiting for %q; got so far: %q", timeout, substr, acc.String())
		case <-ticker.C:
		}
	}
}

// runTerminalE2E starts a real broker (real pty + real ptyMux over a real TCP
// listener, exactly what RunPTY/RunHeadlessPTY wire up) fronting the scripted
// child, dials GET /ws with a real WebSocket client, and drives the full
// round trip: modal arrives at the client, a single-select answer byte sent
// by the client reaches the child, and the child's post-answer marker flows
// back out to the client.
func runTerminalE2E(t *testing.T) {
	t.Helper()

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), scriptedChildEnv+"=1")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	defer func() { _ = ptmx.Close() }()
	defer func() { _ = cmd.Wait() }()

	hub := newPtyHub(256 * 1024)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				_, _ = hub.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	tok := newRotatingToken("e2e-secret")
	srv, port, err := startPtyHTTPServer(ptmx, hub, tok)
	if err != nil {
		t.Fatalf("startPtyHTTPServer: %v", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + tok.value()}},
	})
	if err != nil {
		t.Fatalf("dial broker /ws: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	acc := &accumulator{}
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			acc.append(data)
		}
	}()

	// Step 1: the child's modal render must arrive at the WS client — proves
	// child stdout -> real pty -> ptyHub -> broker /ws -> WS client.
	waitForSubstring(t, acc, "Pick a colour", 5*time.Second)
	waitForSubstring(t, acc, "1. Red", 5*time.Second)

	// Step 2: send a single-select answer for option 1, matching
	// encodeAnswer({mode:'single', index:0}) === ['1'] — one binary WS frame
	// carrying the raw byte "1", no trailing CR (the real client's keystroke
	// model sends single-select as an instant, unterminated digit).
	if err := c.Write(ctx, websocket.MessageBinary, []byte("1")); err != nil {
		t.Fatalf("write answer frame: %v", err)
	}

	// Step 3: the child's post-answer marker must arrive at the WS client —
	// proves client keystroke -> broker -> pty -> child stdin -> child reacts
	// -> pty output -> hub -> WS -> client. This is the full round trip.
	waitForSubstring(t, acc, "PROCEEDED:answer-received", 5*time.Second)
}

func TestTerminalE2E_QuestionAnsweredOverBroker(t *testing.T) {
	runTerminalE2E(t)
}

// TestTerminalE2E_QuestionAnsweredOverBroker_Repeat guards against flakiness
// in the scripted-child transport: pty scheduling and OS-level buffering can
// vary run to run, so this repeats the full round trip a handful of times in
// one test process (cheaper than -count=N across a whole `go test` process).
func TestTerminalE2E_QuestionAnsweredOverBroker_Repeat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping repeat flakiness guard in -short mode")
	}
	for i := 0; i < 5; i++ {
		i := i
		t.Run(fmt.Sprintf("run-%d", i), func(t *testing.T) {
			runTerminalE2E(t)
		})
	}
}
