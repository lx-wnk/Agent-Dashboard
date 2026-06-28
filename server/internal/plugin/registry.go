package plugin

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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// pluginIDRe restricts plugin IDs to lowercase alphanumeric and hyphens, starting
// with an alphanumeric character. This prevents path traversal via malformed IDs.
var pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Registry discovers, starts, and health-checks plugins from a directory.
type Registry struct {
	mu                    sync.RWMutex
	dir                   string
	plugins               []Entry
	attemptedCapabilities map[string]bool // capabilities seen in any plugin.json, regardless of health
	enabled               func(id string) bool
	serverCtx             context.Context
	hooks                 Hooks
}

// Entry is a loaded plugin with its descriptor and running process (if started by us).
type Entry struct {
	Descriptor Descriptor
	cmd        *exec.Cmd
	// cmdDone is closed by the watchPlugin goroutine when cmd.Wait() returns.
	// It is nil when no watcher runs (no Command field in descriptor).
	// Shutdown waits on this channel instead of calling cmd.Wait() itself,
	// preventing two goroutines from calling Wait() on the same *exec.Cmd
	// (which is undefined behavior in Go).
	cmdDone      chan struct{}
	BaseURL      string // http://{addr}
	restartCount int
	pluginDir string // directory containing plugin.json, needed for restarts

	// healthy is true once the process passed its health check and false once a
	// give-up path (exhausted restarts / failed restart) marks it dead. The
	// dispatcher serves 503 for an unhealthy entry.
	healthy bool
	// intentionalStop is set by StopOne before signalling so the watcher knows
	// the exit was deliberate and must NOT respawn (the real orphan-restart fix).
	intentionalStop bool
}

// New creates a Registry that will discover plugins in dir.
// If dir is empty, the registry does nothing (no plugins).
func New(dir string) *Registry {
	return &Registry{
		dir:                   dir,
		attemptedCapabilities: make(map[string]bool),
	}
}

// SetEnabled installs the predicate that decides which plugins Load starts and
// records. Defaults to all-enabled if never set (callers should set it).
func (r *Registry) SetEnabled(fn func(id string) bool) { r.enabled = fn }

func (r *Registry) isEnabled(id string) bool {
	if r.enabled == nil {
		return true
	}
	return r.enabled(id)
}

// Load scans dir, starts each plugin process, and performs health checks.
// Call once during server startup. serverCtx is the server's lifetime context
// and is cancelled on SIGTERM/SIGINT. Health checks run with an internal
// 30-second timeout derived from serverCtx.
func (r *Registry) Load(serverCtx context.Context, hooks Hooks) error {
	startupCtx, cancel := context.WithTimeout(serverCtx, 30*time.Second)
	defer cancel()
	r.serverCtx = serverCtx
	r.hooks = hooks
	if r.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no plugin dir is fine
		}
		return fmt.Errorf("plugin: read dir %s: %w", r.dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		descPath := filepath.Join(r.dir, entry.Name(), "plugin.json")
		data, err := os.ReadFile(descPath)
		if err != nil {
			slog.Warn("plugin: skip — no plugin.json", "dir", entry.Name())
			continue
		}
		var desc Descriptor
		if err := json.Unmarshal(data, &desc); err != nil {
			slog.Warn("plugin: skip — invalid plugin.json", "dir", entry.Name(), "err", err)
			continue
		}
		if !pluginIDRe.MatchString(desc.ID) {
			slog.Warn("plugin: skip — id must match ^[a-z0-9][a-z0-9-]*$", "dir", entry.Name(), "id", desc.ID)
			continue
		}
		if !r.isEnabled(desc.ID) {
			slog.Info("plugin: skip — disabled", "id", desc.ID)
			continue
		}
		// Record every capability seen in plugin.json regardless of whether the
		// plugin passes health-check. Used by HasAttemptedCapability so callers
		// can distinguish "no plugin configured" from "plugin configured but broken".
		for _, cap := range desc.Capabilities {
			r.attemptedCapabilities[cap] = true
		}
		pluginDir := filepath.Join(r.dir, entry.Name())
		if err := r.startEntry(serverCtx, startupCtx, pluginDir, desc, hooks); err != nil {
			slog.Error("plugin: skip — startup failed", "id", desc.ID, "err", err)
			continue
		}
		slog.Info("plugin: loaded", "id", desc.ID, "capabilities", desc.Capabilities)
	}
	return nil
}

// startEntry starts (if it has a Command), health-checks, registers, and wires
// hooks for one descriptor. The caller holds no lock.
func (r *Registry) startEntry(serverCtx, startupCtx context.Context, pluginDir string, desc Descriptor, hooks Hooks) error {
	host, _, err := net.SplitHostPort(desc.Addr)
	if err != nil {
		return fmt.Errorf("invalid addr %q: %w", desc.Addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("addr %q must be loopback", desc.Addr)
	}
	entry := Entry{Descriptor: desc, BaseURL: "http://" + desc.Addr, pluginDir: pluginDir}
	if len(desc.Command) > 0 {
		cmd := exec.CommandContext(serverCtx, desc.Command[0], desc.Command[1:]...)
		cmd.Dir = pluginDir
		cmd.Env = buildPluginEnv(desc.Env)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		entry.cmd = cmd
	}
	if err := r.waitHealthy(startupCtx, entry.BaseURL); err != nil {
		if entry.cmd != nil {
			gracefulStop(entry.cmd, nil)
		}
		return fmt.Errorf("health: %w", err)
	}
	if entry.cmd != nil {
		// Health check passed — safe to launch the watcher now. Starting it
		// before the health check would race with gracefulStop (both call
		// cmd.Wait). Exponential backoff: 1s → 5s → 30s, max 3 retries.
		done := make(chan struct{})
		entry.cmdDone = done
		go r.watchPlugin(serverCtx, entry.pluginDir, desc, entry.cmd, done)
	}
	entry.healthy = true
	r.mu.Lock()
	r.plugins = append(r.plugins, entry)
	r.mu.Unlock()
	if desc.HasCapability(CapAuthProvider) && hooks.SetAuth != nil {
		hooks.SetAuth(NewAuthProvider(entry), entry.BaseURL+"/login")
	}
	return nil
}

// StartOne starts a single plugin by id (reads its plugin.json fresh from the
// dir). Used by the live-enable path. No-op if already running.
func (r *Registry) StartOne(ctx context.Context, id string) error {
	if !pluginIDRe.MatchString(id) {
		return fmt.Errorf("plugin: invalid id %q", id)
	}
	r.mu.RLock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			r.mu.RUnlock()
			return nil // already running
		}
	}
	serverCtx, hooks := r.serverCtx, r.hooks
	r.mu.RUnlock()
	if serverCtx == nil {
		serverCtx = ctx
	}
	descPath := filepath.Join(r.dir, id, "plugin.json")
	data, err := os.ReadFile(descPath)
	if err != nil {
		return fmt.Errorf("plugin: read %s: %w", descPath, err)
	}
	var desc Descriptor
	if err := json.Unmarshal(data, &desc); err != nil {
		return fmt.Errorf("plugin: invalid plugin.json for %q: %w", id, err)
	}
	if desc.ID != id {
		return fmt.Errorf("plugin: id mismatch in %s", descPath)
	}
	r.mu.Lock()
	for _, c := range desc.Capabilities {
		r.attemptedCapabilities[c] = true
	}
	r.mu.Unlock()
	startupCtx, cancel := context.WithTimeout(serverCtx, 30*time.Second)
	defer cancel()
	return r.startEntry(serverCtx, startupCtx, filepath.Join(r.dir, id), desc, hooks)
}

// StopOne stops and deregisters a single plugin by id. No-op if not running.
func (r *Registry) StopOne(id string) error {
	r.mu.Lock()
	var target *Entry
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			e := r.plugins[i]
			target = &e
			break
		}
	}
	r.mu.Unlock()
	if target == nil {
		return nil
	}
	r.setIntentionalStop(id)
	if target.cmd != nil {
		gracefulStop(target.cmd, target.cmdDone)
	}
	r.removeByID(id)
	return nil
}

// setIntentionalStop marks the entry's pending exit as deliberate so watchPlugin
// will not respawn it.
func (r *Registry) setIntentionalStop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			r.plugins[i].intentionalStop = true
			return
		}
	}
}

// isIntentionalStop reports whether the watcher should refrain from restarting.
// An absent entry counts as intentional: by the time the watcher checks, StopOne
// may already have deregistered it, and a removed plugin must never respawn.
func (r *Registry) isIntentionalStop(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			return r.plugins[i].intentionalStop
		}
	}
	return true
}

// Shutdown stops all plugin processes that were started by Load.
// gracefulStop sends SIGTERM and waits up to 5s before killing. See gracefulStop for details.
func (r *Registry) Shutdown() {
	r.mu.Lock()
	plugins := make([]Entry, len(r.plugins))
	copy(plugins, r.plugins)
	r.mu.Unlock()

	for _, p := range plugins {
		if p.cmd != nil {
			gracefulStop(p.cmd, p.cmdDone)
		}
	}
}

// gracefulStop sends SIGTERM to the process group led by cmd's PID and waits
// for the process to exit. Setpgid on spawn makes the plugin the group leader
// (pgid == pid), so the negative-pid kill reaches the plugin and all descendants.
// If the process has not exited within 5 seconds, SIGKILL is sent to the group.
// If watcherDone is non-nil it is the channel closed by the watchPlugin goroutine
// that owns cmd.Wait(); gracefulStop waits on it to avoid a double-Wait race.
func gracefulStop(cmd *exec.Cmd, watcherDone <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	signalGroup(cmd.Process.Pid, syscall.SIGTERM)

	done := watcherDone
	if done == nil {
		ownDone := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(ownDone)
		}()
		done = ownDone
	}

	go func() {
		select {
		case <-done:
			// process exited — nothing to do
		case <-time.After(5 * time.Second):
			signalGroup(cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
}

// signalGroup sends sig to the process group led by pid (Setpgid makes the child
// its own group leader, so its pgid == pid). The negative target reaches the
// leader and all descendants, so a plugin's child processes die with it.
// Falls back to the single process if the group send fails.
func signalGroup(pid int, sig syscall.Signal) {
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = syscall.Kill(pid, sig)
	}
}

// FindByCapability returns the first plugin with the given capability, or nil.
func (r *Registry) FindByCapability(capability string) *Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.HasCapability(capability) {
			return &r.plugins[i]
		}
	}
	return nil
}

// AllWithCapability returns all plugins with the given capability.
func (r *Registry) AllWithCapability(capability string) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Entry
	for _, p := range r.plugins {
		if p.Descriptor.HasCapability(capability) {
			out = append(out, p)
		}
	}
	return out
}

// HasAttemptedCapability reports whether any plugin.json in the directory
// declared the given capability, regardless of whether that plugin passed
// the health-check and ended up in the registry.
func (r *Registry) HasAttemptedCapability(capability string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.attemptedCapabilities[capability]
}

// HasDir reports whether the registry was constructed with a non-empty plugin directory.
func (r *Registry) HasDir() bool {
	return r.dir != ""
}

// Healthy reports whether this entry's process is currently considered healthy.
func (e Entry) Healthy() bool { return e.healthy }

// Lookup returns a copy of the entry for id and whether it is present. The copy
// avoids handing out a pointer into the value-backed plugins slice (which
// reallocates on append/remove). Plugin count is single-digit, so the linear
// scan under RLock matches the existing All/FindByCapability patterns.
func (r *Registry) Lookup(id string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			return r.plugins[i], true
		}
	}
	return Entry{}, false
}

// InjectEntryForTest registers a synthetic entry without starting a process.
// Test-only seam; production code paths go through startEntry.
func (r *Registry) InjectEntryForTest(d Descriptor, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = append(r.plugins, Entry{Descriptor: d, BaseURL: "http://" + d.Addr, healthy: healthy})
}

// NewHealthyEntryForTest builds a healthy Entry for tests in other packages.
func NewHealthyEntryForTest(d Descriptor) Entry {
	return Entry{Descriptor: d, BaseURL: "http://" + d.Addr, healthy: true}
}

// All returns a snapshot of all loaded plugin entries.
func (r *Registry) All() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, len(r.plugins))
	copy(out, r.plugins)
	return out
}

// Info is a safe, read-only snapshot of a loaded plugin. It intentionally
// excludes internal state (cmd, restartCount) so consumers cannot mutate
// live plugin processes.
type Info struct {
	ID           string
	Capabilities []string
	BaseURL      string
}

// Infos returns a snapshot of all loaded plugins as Info values.
// Use this instead of All() when only metadata is needed.
func (r *Registry) Infos() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.plugins))
	for _, e := range r.plugins {
		out = append(out, Info{
			ID:           e.Descriptor.ID,
			Capabilities: e.Descriptor.Capabilities,
			BaseURL:      e.BaseURL,
		})
	}
	return out
}

// watchPlugin waits for cmd to exit, then attempts to restart it with
// exponential backoff. It gives up after maxPluginRestarts attempts and
// removes the entry from the registry.
const maxPluginRestarts = 3

var restartBackoff = []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second}

func (r *Registry) watchPlugin(ctx context.Context, pluginDir string, desc Descriptor, cmd *exec.Cmd, done chan<- struct{}) {
	restartCount := 0
	current := cmd

	firstWait := true
	for {
		err := current.Wait()
		if firstWait {
			// Signal Shutdown that the initial cmd has exited. Must happen exactly
			// once so Shutdown's gracefulStop doesn't hang on a never-closed channel.
			close(done)
			firstWait = false
		}
		if r.isIntentionalStop(desc.ID) {
			return
		}
		// A nil error means the plugin exited cleanly (e.g. during Shutdown).
		// Only attempt restarts on non-nil errors.
		if err == nil {
			return
		}
		// If the server is shutting down, the SIGTERM we sent caused this exit —
		// not an unexpected crash. Return silently to avoid a spurious error log.
		if ctx.Err() != nil {
			return
		}
		slog.Error("plugin: process exited unexpectedly", "id", desc.ID, "err", err)

		if restartCount >= maxPluginRestarts {
			slog.Error("plugin: exceeded restart limit — marking unhealthy", "id", desc.ID, "restarts", restartCount)
			r.markUnhealthy(desc.ID)
			return
		}

		delay := restartBackoff[min(restartCount, len(restartBackoff)-1)]
		slog.Info("plugin: restarting after backoff", "id", desc.ID, "attempt", restartCount+1, "delay", delay)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		newCmd := exec.CommandContext(ctx, desc.Command[0], desc.Command[1:]...)
		newCmd.Dir = pluginDir
		newCmd.Env = buildPluginEnv(desc.Env)
		newCmd.Stdout = os.Stdout
		newCmd.Stderr = os.Stderr
		newCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if startErr := newCmd.Start(); startErr != nil {
			slog.Error("plugin: restart failed — could not start process", "id", desc.ID, "err", startErr)
			r.markUnhealthy(desc.ID)
			return
		}

		baseURL := "http://" + desc.Addr
		if healthErr := r.waitHealthy(ctx, baseURL); healthErr != nil {
			slog.Error("plugin: restart failed — health check did not pass", "id", desc.ID, "err", healthErr)
			// newCmd has no watcher goroutine; pass nil so gracefulStop owns Wait().
			gracefulStop(newCmd, nil)
			r.markUnhealthy(desc.ID)
			return
		}

		restartCount++
		slog.Info("plugin: restarted successfully", "id", desc.ID, "restartCount", restartCount)

		r.mu.Lock()
		for i := range r.plugins {
			if r.plugins[i].Descriptor.ID == desc.ID {
				r.plugins[i].cmd = newCmd
				r.plugins[i].restartCount = restartCount
				r.plugins[i].healthy = true
				break
			}
		}
		r.mu.Unlock()

		current = newCmd
	}
}

// markUnhealthy flips the entry to unhealthy and notifies the OnUnhealthy hook.
// The entry is kept so the dispatcher serves a knowing 503 rather than the
// ambiguous 503 of a missing plugin.
func (r *Registry) markUnhealthy(id string) {
	r.mu.Lock()
	for i := range r.plugins {
		if r.plugins[i].Descriptor.ID == id {
			r.plugins[i].healthy = false
			break
		}
	}
	hook := r.hooks.OnUnhealthy
	r.mu.Unlock()
	if hook != nil {
		hook(id)
	}
}

// removeByID removes a plugin entry from the registry by plugin ID.
func (r *Registry) removeByID(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.plugins {
		if p.Descriptor.ID == id {
			r.plugins = append(r.plugins[:i], r.plugins[i+1:]...)
			return
		}
	}
}

// buildPluginEnv constructs a minimal environment for a plugin process.
// It exposes only a safe base set of env vars plus any keys explicitly
// allow-listed in the plugin's descriptor (desc.Env).
func buildPluginEnv(allowedKeys []string) []string {
	base := []string{"PATH", "HOME", "TMPDIR", "TEMP", "USER", "LANG", "LC_ALL"}
	allowed := make(map[string]bool, len(base)+len(allowedKeys))
	for _, k := range base {
		allowed[k] = true
	}
	for _, k := range allowedKeys {
		allowed[k] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		if idx := strings.Index(kv, "="); idx > 0 {
			if allowed[kv[:idx]] {
				env = append(env, kv)
			}
		}
	}
	return env
}

func (r *Registry) waitHealthy(ctx context.Context, baseURL string) error {
	healthClient := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := healthClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("plugin at %s did not become healthy within 5s", baseURL)
}
