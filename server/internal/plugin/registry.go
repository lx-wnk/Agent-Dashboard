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
	mu                   sync.RWMutex
	dir                  string
	plugins              []Entry
	attemptedCapabilities map[string]bool // capabilities seen in any plugin.json, regardless of health
}

// Entry is a loaded plugin with its descriptor and running process (if started by us).
type Entry struct {
	Descriptor   Descriptor
	cmd          *exec.Cmd
	BaseURL      string // http://{addr}
	restartCount int
	pluginDir    string // directory containing plugin.json, needed for restarts
}

// New creates a Registry that will discover plugins in dir.
// If dir is empty, the registry does nothing (no plugins).
func New(dir string) *Registry {
	return &Registry{
		dir:                  dir,
		attemptedCapabilities: make(map[string]bool),
	}
}

// Load scans dir, starts each plugin process, and performs health checks.
// Call once during server startup. serverCtx is the server's lifetime context
// and is cancelled on SIGTERM/SIGINT. Health checks run with an internal
// 30-second timeout derived from serverCtx.
func (r *Registry) Load(serverCtx context.Context, hooks Hooks) error {
	startupCtx, cancel := context.WithTimeout(serverCtx, 30*time.Second)
	defer cancel()
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
		// Record every capability seen in plugin.json regardless of whether the
		// plugin passes health-check. Used by HasAttemptedCapability so callers
		// can distinguish "no plugin configured" from "plugin configured but broken".
		for _, cap := range desc.Capabilities {
			r.attemptedCapabilities[cap] = true
		}
		host, _, err := net.SplitHostPort(desc.Addr)
		if err != nil {
			slog.Warn("plugin: invalid addr format, skipping", "id", desc.ID, "addr", desc.Addr, "err", err)
			continue
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			slog.Warn("plugin: addr must be loopback, skipping", "id", desc.ID, "addr", desc.Addr)
			continue
		}
		pluginEntry := Entry{
			Descriptor: desc,
			BaseURL:    "http://" + desc.Addr,
			pluginDir:  filepath.Join(r.dir, entry.Name()),
		}
		if len(desc.Command) > 0 {
			cmd := exec.CommandContext(serverCtx, desc.Command[0], desc.Command[1:]...)
			cmd.Dir = pluginEntry.pluginDir
			cmd.Env = buildPluginEnv(desc.Env)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				slog.Error("plugin: failed to start", "id", desc.ID, "err", err)
				continue
			}
			pluginEntry.cmd = cmd
			// Watch the process; attempt auto-restart with exponential backoff on
			// unexpected exit (max 3 retries: 1s → 5s → 30s).
			go r.watchPlugin(serverCtx, pluginEntry.pluginDir, desc, cmd)
		}
		if err := r.waitHealthy(startupCtx, pluginEntry.BaseURL); err != nil {
			slog.Error("plugin: health check failed", "id", desc.ID, "err", err)
			if pluginEntry.cmd != nil && pluginEntry.cmd.Process != nil {
				_ = pluginEntry.cmd.Process.Signal(syscall.SIGTERM)
				time.AfterFunc(3*time.Second, func() {
					_ = pluginEntry.cmd.Process.Kill()
				})
			}
			continue
		}
		slog.Info("plugin: loaded", "id", desc.ID, "capabilities", desc.Capabilities)
		r.mu.Lock()
		r.plugins = append(r.plugins, pluginEntry)
		r.mu.Unlock()

		// Self-registration via hooks — notify callers about newly discovered capabilities.
		if desc.HasCapability(CapAuthProvider) && hooks.SetAuth != nil {
			hooks.SetAuth(NewAuthProvider(pluginEntry))
		}
	}
	return nil
}

// Shutdown stops all plugin processes that were started by Load.
// cmd.Wait() is handled by the goroutine launched after cmd.Start(),
// so we only need to signal the process here.
func (r *Registry) Shutdown() {
	r.mu.Lock()
	plugins := make([]Entry, len(r.plugins))
	copy(plugins, r.plugins)
	r.mu.Unlock()

	for _, p := range plugins {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
			time.AfterFunc(3*time.Second, func() {
				_ = p.cmd.Process.Kill()
			})
		}
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

func (r *Registry) watchPlugin(ctx context.Context, pluginDir string, desc Descriptor, cmd *exec.Cmd) {
	restartCount := 0
	current := cmd

	for {
		err := current.Wait()
		// A nil error means the plugin exited cleanly (e.g. during Shutdown).
		// Only attempt restarts on non-nil errors.
		if err == nil {
			return
		}
		slog.Error("plugin: process exited unexpectedly", "id", desc.ID, "err", err)

		if restartCount >= maxPluginRestarts {
			slog.Error("plugin: exceeded restart limit — removing from registry", "id", desc.ID, "restarts", restartCount)
			r.removeByID(desc.ID)
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

		if startErr := newCmd.Start(); startErr != nil {
			slog.Error("plugin: restart failed — could not start process", "id", desc.ID, "err", startErr)
			r.removeByID(desc.ID)
			return
		}

		baseURL := "http://" + desc.Addr
		if healthErr := r.waitHealthy(ctx, baseURL); healthErr != nil {
			slog.Error("plugin: restart failed — health check did not pass", "id", desc.ID, "err", healthErr)
			_ = newCmd.Process.Signal(syscall.SIGTERM)
			time.AfterFunc(3*time.Second, func() { _ = newCmd.Process.Kill() })
			r.removeByID(desc.ID)
			return
		}

		restartCount++
		slog.Info("plugin: restarted successfully", "id", desc.ID, "restartCount", restartCount)

		r.mu.Lock()
		for i := range r.plugins {
			if r.plugins[i].Descriptor.ID == desc.ID {
				r.plugins[i].cmd = newCmd
				r.plugins[i].restartCount = restartCount
				break
			}
		}
		r.mu.Unlock()

		current = newCmd
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
