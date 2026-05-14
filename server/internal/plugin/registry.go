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
	"strings"
	"sync"
	"syscall"
	"time"
)

// Registry discovers, starts, and health-checks plugins from a directory.
type Registry struct {
	mu      sync.RWMutex
	dir     string
	plugins []Entry
}

// Entry is a loaded plugin with its descriptor and running process (if started by us).
type Entry struct {
	Descriptor Descriptor
	cmd        *exec.Cmd
	BaseURL    string // http://{addr}
}

// New creates a Registry that will discover plugins in dir.
// If dir is empty, the registry does nothing (no plugins).
func New(dir string) *Registry {
	return &Registry{dir: dir}
}

// Load scans dir, starts each plugin process, and waits for health.
// Call once during server startup. ctx is used for health-check timeouts.
func (r *Registry) Load(ctx context.Context) error {
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
		}
		if len(desc.Command) > 0 {
			cmd := exec.CommandContext(ctx, desc.Command[0], desc.Command[1:]...)
			cmd.Dir = filepath.Join(r.dir, entry.Name())
			cmd.Env = buildPluginEnv(desc.Env)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				slog.Error("plugin: failed to start", "id", desc.ID, "err", err)
				continue
			}
			// Reap the process when it exits to avoid zombie entries.
			go func() {
				if err := cmd.Wait(); err != nil {
					slog.Error("plugin: process exited unexpectedly", "id", desc.ID, "err", err)
				}
			}()
			pluginEntry.cmd = cmd
		}
		if err := r.waitHealthy(ctx, pluginEntry.BaseURL); err != nil {
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
