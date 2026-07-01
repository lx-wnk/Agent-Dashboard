package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidTmuxPane(t *testing.T) {
	require.True(t, validTmuxPane("%0"))
	require.True(t, validTmuxPane("%42"))
	require.False(t, validTmuxPane(""))
	require.False(t, validTmuxPane("0"))
	require.False(t, validTmuxPane("%1; rm -rf /"))
	require.False(t, validTmuxPane("session:0.1"))
}

func TestTmuxSendArgs(t *testing.T) {
	text, enter := tmuxSendArgs("/tmp/tmux-501/default", "%3", "/security-review")
	require.Equal(t, []string{"-S", "/tmp/tmux-501/default", "send-keys", "-t", "%3", "-l", "--", "/security-review"}, text)
	require.Equal(t, []string{"-S", "/tmp/tmux-501/default", "send-keys", "-t", "%3", "Enter"}, enter)

	// no socket → no -S
	text2, enter2 := tmuxSendArgs("", "%1", "hi")
	require.Equal(t, []string{"send-keys", "-t", "%1", "-l", "--", "hi"}, text2)
	require.Equal(t, []string{"send-keys", "-t", "%1", "Enter"}, enter2)
}

func TestSendKeysToTmux_RunsTextThenEnter(t *testing.T) {
	var calls [][]string
	origRun, origLook := tmuxRunner, tmuxLookPath
	t.Cleanup(func() { tmuxRunner = origRun; tmuxLookPath = origLook })
	tmuxLookPath = func() (string, error) { return "/usr/bin/tmux", nil }
	tmuxRunner = func(_ context.Context, args ...string) error {
		calls = append(calls, args)
		return nil
	}

	err := sendKeysToTmux(context.Background(), "", "%5", "run it")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	require.Equal(t, "-l", calls[0][3]) // literal text send
	require.Equal(t, "run it", calls[0][len(calls[0])-1])
	require.Equal(t, "Enter", calls[1][len(calls[1])-1]) // separate Enter
}

func TestSendKeysToTmux_TmuxMissing(t *testing.T) {
	ran := false
	origRun, origLook := tmuxRunner, tmuxLookPath
	t.Cleanup(func() { tmuxRunner = origRun; tmuxLookPath = origLook })
	tmuxRunner = func(_ context.Context, _ ...string) error { ran = true; return nil }
	tmuxLookPath = func() (string, error) { return "", errors.New("not found") }

	err := sendKeysToTmux(context.Background(), "", "%2", "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "tmux is required")
	require.False(t, ran, "must not attempt send when tmux is absent")
}

func TestSendKeysToTmux_RejectsBadPane(t *testing.T) {
	called := false
	orig := tmuxRunner
	t.Cleanup(func() { tmuxRunner = orig })
	tmuxRunner = func(_ context.Context, _ ...string) error { called = true; return nil }

	err := sendKeysToTmux(context.Background(), "", "$(evil)", "x")
	require.Error(t, err)
	require.False(t, called, "must not exec tmux for an invalid pane")
}

func TestAnswerKeyArgs(t *testing.T) {
	// literal char → -l typed input
	require.Equal(t, []string{"send-keys", "-t", "%1", "-l", "--", "2"}, answerKeyArgs("", "%1", AnswerKey{Char: "2"}))
	// literal text → -l typed input, same as Char
	require.Equal(t, []string{"send-keys", "-t", "%1", "-l", "--", "Cherry"}, answerKeyArgs("", "%1", AnswerKey{Text: "Cherry"}))
	// named key → key name, no -l
	require.Equal(t, []string{"send-keys", "-t", "%1", "Space"}, answerKeyArgs("", "%1", AnswerKey{Named: "Space"}))
	require.Equal(t, []string{"send-keys", "-t", "%1", "Tab"}, answerKeyArgs("", "%1", AnswerKey{Named: "Tab"}))
	// socket prefixes -S
	require.Equal(t, []string{"-S", "/s", "send-keys", "-t", "%1", "Enter"}, answerKeyArgs("/s", "%1", AnswerKey{Named: "Enter"}))
}

func TestSendAnswerKeysToTmux_SendsInOrderWithStepBetweenQuestions(t *testing.T) {
	var calls [][]string
	sleeps := 0
	origRun, origLook, origSleep := tmuxRunner, tmuxLookPath, answerKeyStepSleep
	t.Cleanup(func() { tmuxRunner = origRun; tmuxLookPath = origLook; answerKeyStepSleep = origSleep })
	tmuxLookPath = func() (string, error) { return "/usr/bin/tmux", nil }
	tmuxRunner = func(_ context.Context, args ...string) error { calls = append(calls, args); return nil }
	answerKeyStepSleep = func() { sleeps++ }

	batches := [][]AnswerKey{
		{{Char: "1"}},
		{{Named: "Space"}, {Named: "Enter"}},
	}
	require.NoError(t, sendAnswerKeysToTmux(context.Background(), "", "%5", batches))

	require.Len(t, calls, 3) // "1", then Space, Enter
	require.Equal(t, "1", calls[0][len(calls[0])-1])
	require.Equal(t, "Space", calls[1][len(calls[1])-1])
	require.Equal(t, "Enter", calls[2][len(calls[2])-1])
	require.Equal(t, 1, sleeps, "one inter-question pause between two questions")
}
