package agents

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// tmuxPaneRE matches a tmux pane id (e.g. "%3"). The bridge records $TMUX_PANE,
// which is always of this form; restricting to it keeps the value safe to pass
// as a `tmux send-keys -t` target.
var tmuxPaneRE = regexp.MustCompile(`^%\d+$`)

// tmuxRunner executes a tmux command; indirected for tests.
var tmuxRunner = func(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "tmux", args...).Run()
}

// validTmuxPane reports whether pane is a well-formed tmux pane id.
func validTmuxPane(pane string) bool {
	return tmuxPaneRE.MatchString(pane)
}

// tmuxSendArgs builds the two tmux arg vectors that inject message as real
// keyboard input into the pane: the literal text, then a separate Enter.
// socket may be "" (default tmux server). The message is passed literally (-l)
// after "--" so it is never interpreted as options or key names.
func tmuxSendArgs(socket, pane, message string) (textArgs, enterArgs []string) {
	var base []string
	if socket != "" {
		base = []string{"-S", socket}
	}
	textArgs = append(append([]string{}, base...), "send-keys", "-t", pane, "-l", "--", message)
	enterArgs = append(append([]string{}, base...), "send-keys", "-t", pane, "Enter")
	return textArgs, enterArgs
}

// tmuxLookPath resolves the tmux binary; indirected for tests.
var tmuxLookPath = func() (string, error) { return exec.LookPath("tmux") }

// sendKeysToTmux injects message into the given tmux pane as typed input,
// followed by Enter, so an interactive Claude session executes it.
func sendKeysToTmux(ctx context.Context, socket, pane, message string) error {
	if !validTmuxPane(pane) {
		return fmt.Errorf("invalid tmux pane %q", pane)
	}
	if strings.ContainsAny(socket, "\n\x00") {
		return fmt.Errorf("invalid tmux socket")
	}
	// tmux is a hard requirement for live prompt injection; surface a clear
	// message rather than a raw exec-not-found error.
	if _, err := tmuxLookPath(); err != nil {
		return fmt.Errorf("tmux is required for live prompt injection but was not found on the server PATH; install tmux or send will resume the session instead")
	}
	textArgs, enterArgs := tmuxSendArgs(socket, pane, message)
	if err := tmuxRunner(ctx, textArgs...); err != nil {
		return fmt.Errorf("tmux send-keys (text): %w", err)
	}
	if err := tmuxRunner(ctx, enterArgs...); err != nil {
		return fmt.Errorf("tmux send-keys (enter): %w", err)
	}
	return nil
}

// AnswerKey is one keystroke in an interactive-question answer. Exactly one of
// Char (a single literal character, e.g. "3"), Text (a literal string typed
// into a "Type something" / "Chat about this" row), or Named (a logical key —
// "Tab"/"Enter"/"Space"/"Up"/"Down") is set.
type AnswerKey struct {
	Char  string
	Named string
	Text  string
}

// answerKeyStepDelay is the pause between questions so the next selector renders
// before its keys are sent. Indirected via answerKeyStepSleep for tests.
var answerKeyStepDelay = 250 * time.Millisecond

var answerKeyStepSleep = func() { time.Sleep(answerKeyStepDelay) }

// answerKeyArgs builds the tmux send-keys argv for a single AnswerKey. A literal
// character or text string is sent with -l (typed input); a named key is sent
// as a key name so tmux translates it (Tab/Down/Space/Enter are real
// keypresses, not text).
func answerKeyArgs(socket, pane string, k AnswerKey) []string {
	var base []string
	if socket != "" {
		base = []string{"-S", socket}
	}
	switch {
	case k.Text != "":
		return append(append([]string{}, base...), "send-keys", "-t", pane, "-l", "--", k.Text)
	case k.Char != "":
		return append(append([]string{}, base...), "send-keys", "-t", pane, "-l", "--", k.Char)
	default:
		return append(append([]string{}, base...), "send-keys", "-t", pane, k.Named)
	}
}

// sendAnswerKeysToTmux delivers one key batch per question into the pane, pausing
// between questions so each selector has rendered before its keys arrive.
func sendAnswerKeysToTmux(ctx context.Context, socket, pane string, batches [][]AnswerKey) error {
	if !validTmuxPane(pane) {
		return fmt.Errorf("invalid tmux pane %q", pane)
	}
	if strings.ContainsAny(socket, "\n\x00") {
		return fmt.Errorf("invalid tmux socket")
	}
	if _, err := tmuxLookPath(); err != nil {
		return fmt.Errorf("tmux is required to answer interactive questions but was not found on the server PATH")
	}
	for bi, batch := range batches {
		for _, k := range batch {
			if err := tmuxRunner(ctx, answerKeyArgs(socket, pane, k)...); err != nil {
				return fmt.Errorf("tmux send-keys (answer): %w", err)
			}
		}
		if bi < len(batches)-1 {
			answerKeyStepSleep()
		}
	}
	return nil
}
