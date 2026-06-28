package plugin_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

func TestRegistry_EmptyDir_LoadsNothing(t *testing.T) {
	dir := t.TempDir()
	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	assert.Nil(t, r.FindByCapability(plugin.CapAuthProvider))
}

func TestRegistry_NonexistentDir_NoError(t *testing.T) {
	r := plugin.New("/does/not/exist")
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
}

func TestRegistry_EmptyPluginDir_Skipped(t *testing.T) {
	r := plugin.New("")
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
}

func TestRegistry_InvalidPluginID_Uppercase_Skipped(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "MY-PLUGIN",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         "127.0.0.1:19001",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
}

func TestRegistry_InvalidPluginID_WithSpace_Skipped(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "my plugin",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         "127.0.0.1:19001",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
}

func TestRegistry_NonLoopbackIP_Skipped(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "valid-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "valid-plugin",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         "192.168.1.100:8080",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
}

func TestRegistry_PluginWithHealthy_Loaded(t *testing.T) {
	// Start a real HTTP server acting as a plugin.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Extract host:port from srv.URL
	addr := srv.Listener.Addr().String()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "test-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "test-plugin",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         addr,
		// No Command — server already running
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))

	entry := r.FindByCapability(plugin.CapRouteExtension)
	require.NotNil(t, entry)
	assert.Equal(t, "test-plugin", entry.Descriptor.ID)
}

func writePluginJSON(t *testing.T, dir, id string, caps []string) {
	t.Helper()
	pluginDir := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	desc := plugin.Descriptor{
		ID:           id,
		Version:      "1.0.0",
		Capabilities: caps,
		Addr:         "127.0.0.1:0",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))
}

// startHealthStub starts an in-process HTTP server on 127.0.0.1 that returns
// 200 on /health, registers cleanup, and returns it for its address.
func startHealthStub(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// writePluginJSONAddr writes a plugin.json with the given addr and NO Command
// field, so the registry only health-probes and never spawns a subprocess.
func writePluginJSONAddr(t *testing.T, dir, id string, caps []string, addr string) {
	t.Helper()
	pluginDir := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	desc := plugin.Descriptor{
		ID:           id,
		Version:      "1.0.0",
		Capabilities: caps,
		Addr:         addr,
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))
}

func TestRegistry_StartOneStopOne(t *testing.T) {
	dir := t.TempDir()
	ts := startHealthStub(t)
	addr := strings.TrimPrefix(ts.URL, "http://")
	writePluginJSONAddr(t, dir, "live-plugin", []string{plugin.CapRouteExtension}, addr)

	r := plugin.New(dir)
	r.SetEnabled(func(string) bool { return false })
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))

	require.NoError(t, r.StartOne(context.Background(), "live-plugin"))
	require.NotNil(t, r.FindByCapability(plugin.CapRouteExtension))

	// Idempotent start.
	require.NoError(t, r.StartOne(context.Background(), "live-plugin"))
	require.NotNil(t, r.FindByCapability(plugin.CapRouteExtension))

	require.NoError(t, r.StopOne("live-plugin"))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))

	// Idempotent stop.
	require.NoError(t, r.StopOne("live-plugin"))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
}

// writeHealthyPlugin creates a plugin dir under dir/{id}/plugin.json backed by
// a real in-process HTTP server that returns 200 on every path including /health.
// Reused by Tasks 2, 5, 6, 7, 8.
func writeHealthyPlugin(t *testing.T, dir, id string) {
	t.Helper()
	ts := startHealthStub(t)
	addr := strings.TrimPrefix(ts.URL, "http://")
	writePluginJSONAddr(t, dir, id, []string{plugin.CapRouteExtension}, addr)
}

func TestStartedPluginIsHealthy(t *testing.T) {
	dir := t.TempDir()
	writeHealthyPlugin(t, dir, "alive")

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	got, ok := r.Lookup("alive")
	require.True(t, ok)
	require.True(t, got.Healthy())
}

func TestLookupReturnsHealthyEntryCopy(t *testing.T) {
	r := plugin.New("")
	require.False(t, exists(r, "missing"))

	// Inject a running, healthy entry through the exported test seam.
	r.InjectEntryForTest(plugin.Descriptor{ID: "voice", Addr: "127.0.0.1:19010"}, true)

	got, ok := r.Lookup("voice")
	require.True(t, ok)
	require.Equal(t, "voice", got.Descriptor.ID)
	require.True(t, got.Healthy())

	_, ok = r.Lookup("nope")
	require.False(t, ok)
}

func exists(r *plugin.Registry, id string) bool {
	_, ok := r.Lookup(id)
	return ok
}

func TestRegistry_LoadSkipsDisabled(t *testing.T) {
	dir := t.TempDir()
	writePluginJSON(t, dir, "enabled-one", []string{plugin.CapRouteExtension})
	writePluginJSON(t, dir, "disabled-auth", []string{plugin.CapAuthProvider})

	r := plugin.New(dir)
	r.SetEnabled(func(id string) bool { return id == "enabled-one" })

	// Neither plugin actually starts (no Command/health), but the capability
	// recording must reflect ONLY enabled plugins.
	_ = r.Load(context.Background(), plugin.Hooks{})

	assert.False(t, r.HasAttemptedCapability(plugin.CapAuthProvider),
		"disabled auth_provider plugin must not be recorded as attempted")
}

// writeForkingPlugin builds a real plugin binary under dir/{id}/ that:
//  1. Starts a /health HTTP server on a free port.
//  2. Spawns `sleep 600` as a child WITHOUT a new pgid (inherits the plugin's group).
//  3. Writes the child PID to childPidFile so the test can assert it was reaped.
//
// The binary is pre-compiled via `go build` so it starts fast enough to pass
// the registry's 5-second health-check window.
// Returns the absolute path of the child-PID file.
func writeForkingPlugin(t *testing.T, dir, id string) (childPidFile string) {
	t.Helper()
	pluginDir := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	// Pick a free port once; close the listener before binding in the plugin.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	childPidFile = filepath.Join(pluginDir, "childpid")

	mainGo := `package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
)

func main() {
	addr := os.Args[1]
	pidFile := os.Args[2]

	// Spawn a long-lived child in the SAME process group (no Setpgid on child).
	child := exec.Command("sleep", "600")
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "child start:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", child.Process.Pid)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write pid:", err)
		os.Exit(1)
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "server:", err)
		os.Exit(1)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte(mainGo), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "go.mod"), []byte("module forker-plugin\n\ngo 1.21\n"), 0o644))

	// Pre-compile so the binary starts instantly and health check passes well within 5s.
	binPath := filepath.Join(pluginDir, "plugin-bin")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = pluginDir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := build.CombinedOutput()
	require.NoError(t, buildErr, "go build failed:\n%s", out)

	desc := plugin.Descriptor{
		ID:           id,
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         addr,
		Command:      []string{"./plugin-bin", addr, childPidFile},
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	return childPidFile
}

func readPidFile(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	return pid
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// writeRealHealthyPlugin builds a real plugin binary under dir/{id}/ that serves
// GET /health → 200 and runs until killed. No child processes are spawned.
// Returns the addr the plugin listens on, so callers can probe for orphan processes.
func writeRealHealthyPlugin(t *testing.T, dir, id string) string {
	t.Helper()
	pluginDir := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	mainGo := `package main

import (
	"net/http"
	"os"
)

func main() {
	addr := os.Args[1]
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_ = http.ListenAndServe(addr, nil)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte(mainGo), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "go.mod"), []byte("module healthy-plugin\n\ngo 1.21\n"), 0o644))

	binPath := filepath.Join(pluginDir, "plugin-bin")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = pluginDir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, buildErr := build.CombinedOutput()
	require.NoError(t, buildErr, "go build failed:\n%s", out)

	desc := plugin.Descriptor{
		ID:           id,
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         addr,
		Command:      []string{"./plugin-bin", addr},
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))
	return addr
}

func TestStopOneDoesNotRespawn(t *testing.T) {
	dir := t.TempDir()
	addr := writeRealHealthyPlugin(t, dir, "stoppable")

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	_, ok := r.Lookup("stoppable")
	require.True(t, ok, "plugin must be registered after load")

	require.NoError(t, r.StopOne("stoppable"))

	// Phase 1: wait for the plugin process to actually exit.
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return true // port is closed — process has exited
		}
		conn.Close()
		return false
	}, 3*time.Second, 50*time.Millisecond, "plugin process must exit after StopOne")

	// Phase 2: verify the watcher does not restart the process (watcher's 1s
	// backoff is within the 3s window, so any spurious restart will be caught).
	require.Never(t, func() bool {
		if _, ok := r.Lookup("stoppable"); ok {
			return true // entry reappeared in registry
		}
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return true // orphan process is listening
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "intentional stop must not respawn")
}

func TestGroupKillReapsChildren(t *testing.T) {
	dir := t.TempDir()
	childPidFile := writeForkingPlugin(t, dir, "forker")

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	_, ok := r.Lookup("forker")
	require.True(t, ok, "plugin must be registered after load")

	r.Shutdown()

	childPid := readPidFile(t, childPidFile)
	require.Eventually(t, func() bool {
		return !processAlive(childPid)
	}, 10*time.Second, 100*time.Millisecond, "group-kill must reap the plugin's child process")
}
