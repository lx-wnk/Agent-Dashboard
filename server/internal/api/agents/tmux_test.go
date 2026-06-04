package agents

import (
	"context"
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
	orig := tmuxRunner
	t.Cleanup(func() { tmuxRunner = orig })
	tmuxRunner = func(_ context.Context, args ...string) error {
		calls = append(calls, args)
		return nil
	}

	err := sendKeysToTmux(context.Background(), "", "%5", "run it")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	require.Equal(t, "-l", calls[0][3])               // literal text send
	require.Equal(t, "run it", calls[0][len(calls[0])-1])
	require.Equal(t, "Enter", calls[1][len(calls[1])-1]) // separate Enter
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
