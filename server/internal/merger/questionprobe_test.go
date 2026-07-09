package merger

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
)

// writeProbeDiscovery writes a pty discovery file for pid pointing at addr
// (an httptest server's listener address, e.g. "127.0.0.1:54321") with token.
func writeProbeDiscovery(t *testing.T, home string, pid int, port int, token string) {
	t.Helper()
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	data, err := json.Marshal(map[string]any{"port": port, "token": token, "ptyInject": true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(channelconfig.DiscoveryPtyFile(home, pid), data, 0o600))
}

func TestRealQuestionProbe_200DecodesQuestion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := sdk.DetectedQuestion{
		Header:   "Pick a color",
		Question: "Pick a color?",
		Options: []sdk.DetectedOption{
			{Index: 1, Label: "Red"},
			{Index: 2, Label: "Green"},
		},
		TypeSomethingIndex: 3,
		ChatAboutIndex:     4,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/question", r.URL.Path)
		assert.Equal(t, "Bearer secret-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	writeProbeDiscovery(t, home, 4242, port, "secret-token")

	got := RealQuestionProbe(4242)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestRealQuestionProbe_204ReturnsNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	writeProbeDiscovery(t, home, 4343, port, "secret-token")

	got := RealQuestionProbe(4343)
	assert.Nil(t, got)
}

func TestRealQuestionProbe_401ReturnsNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	writeProbeDiscovery(t, home, 4444, port, "wrong-token")

	got := RealQuestionProbe(4444)
	assert.Nil(t, got)
}

func TestRealQuestionProbe_NoDiscoveryFileReturnsNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := RealQuestionProbe(9999)
	assert.Nil(t, got)
}

func TestRealQuestionProbe_TmuxCapturePaneDetectsQuestion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	pid := 55201
	data, err := json.Marshal(map[string]any{"tmuxPane": "%3", "tmuxSocket": "/tmp/sock"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(channelconfig.DiscoveryFile(home, pid), data, 0o600))

	var gotSocket, gotPane string
	orig := captureTmuxPane
	captureTmuxPane = func(socket, pane string) ([]string, error) {
		gotSocket, gotPane = socket, pane
		return []string{
			"Pick a colour",
			"What is your favourite colour?",
			"1. Red",
			"2. Green",
			"3. Blue",
			"4. Type something",
			"5. Chat about this",
		}, nil
	}
	defer func() { captureTmuxPane = orig }()

	q := RealQuestionProbe(pid)
	require.NotNil(t, q)
	assert.False(t, q.MultiSelect)
	assert.Equal(t, "/tmp/sock", gotSocket)
	assert.Equal(t, "%3", gotPane)
	labels := []string{q.Options[0].Label, q.Options[1].Label, q.Options[2].Label}
	assert.Equal(t, []string{"Red", "Green", "Blue"}, labels)
	assert.Equal(t, 4, q.TypeSomethingIndex)
	assert.Equal(t, 5, q.ChatAboutIndex)
}

func TestRealQuestionProbe_NoTmuxPaneReturnsNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, channelconfig.DiscoveryDir)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	pid := 55202
	data, err := json.Marshal(map[string]any{"tmuxPane": ""})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(channelconfig.DiscoveryFile(home, pid), data, 0o600))

	assert.Nil(t, RealQuestionProbe(pid))
}
