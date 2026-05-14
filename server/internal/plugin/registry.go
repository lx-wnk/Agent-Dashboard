package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Registry discovers, starts, and health-checks plugins from a directory.
type Registry struct {
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
		pluginEntry := Entry{
			Descriptor: desc,
			BaseURL:    "http://" + desc.Addr,
		}
		if len(desc.Command) > 0 {
			cmd := exec.CommandContext(ctx, desc.Command[0], desc.Command[1:]...)
			cmd.Dir = filepath.Join(r.dir, entry.Name())
			cmd.Env = os.Environ() // forward full env so GITHUB_CLIENT_ID etc. are available
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				slog.Error("plugin: failed to start", "id", desc.ID, "err", err)
				continue
			}
			pluginEntry.cmd = cmd
		}
		if err := r.waitHealthy(ctx, pluginEntry.BaseURL); err != nil {
			slog.Error("plugin: health check failed", "id", desc.ID, "err", err)
			if pluginEntry.cmd != nil {
				_ = pluginEntry.cmd.Process.Kill()
			}
			continue
		}
		slog.Info("plugin: loaded", "id", desc.ID, "capabilities", desc.Capabilities)
		r.plugins = append(r.plugins, pluginEntry)
	}
	return nil
}

// Shutdown stops all plugin processes that were started by Load.
func (r *Registry) Shutdown() {
	for _, p := range r.plugins {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
}

// FindByCapability returns the first plugin with the given capability, or nil.
func (r *Registry) FindByCapability(cap string) *Entry {
	for i := range r.plugins {
		if r.plugins[i].Descriptor.HasCapability(cap) {
			return &r.plugins[i]
		}
	}
	return nil
}

// AllWithCapability returns all plugins with the given capability.
func (r *Registry) AllWithCapability(cap string) []Entry {
	var out []Entry
	for _, p := range r.plugins {
		if p.Descriptor.HasCapability(cap) {
			out = append(out, p)
		}
	}
	return out
}

func (r *Registry) waitHealthy(ctx context.Context, baseURL string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
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
