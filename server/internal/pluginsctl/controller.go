// Package pluginsctl is the control plane for plugin enable/disable. Toggling a
// plugin persists the new active-state in the plugin table and takes effect on
// the next server restart; the registry is read-only here (health/info for
// listing).
package pluginsctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// ErrUnknownPlugin signals that an ID matched no discovered plugin. The handler
// maps it to 400; any other error is a persistence failure mapped to 500.
var ErrUnknownPlugin = errors.New("pluginsctl: unknown plugin")

// ErrInvalidAction signals an unsupported lifecycle action (not one of
// install|activate|deactivate|uninstall). The handler maps it to 400.
var ErrInvalidAction = errors.New("pluginsctl: invalid action")

// Applied describes whether a change took effect immediately or needs a restart.
type Applied string

const (
	AppliedLive    Applied = "live"
	AppliedRestart Applied = "restart"
)

// Registry is the minimal subset of *plugin.Registry the controller needs,
// declared locally so tests can fake it. Toggling is restart-to-apply, so only
// the read-only health/info view is used here.
type Registry interface {
	Infos() []plugin.Info
}

// PluginState is the discovered + runtime view of a single plugin.
type PluginState struct {
	ID           string
	Capabilities []string
	Enabled      bool
	Healthy      bool
	AuthProvider bool
}

// Controller resolves discovered plugins and toggles their enable-state.
type Controller struct {
	reg     Registry
	plugins repo.PluginRepo
	dir     string
	// mu serializes the read-modify-write of a plugin row in SetEnabled so
	// concurrent toggles cannot lose an update.
	mu sync.Mutex
}

// New builds a Controller. plugins may be nil (no DB); in that case nothing is
// reported enabled and SetEnabled fails because the state cannot be persisted.
func New(reg Registry, plugins repo.PluginRepo, dir string) *Controller {
	return &Controller{reg: reg, plugins: plugins, dir: dir}
}

// List returns every discovered plugin with its enable/health/auth flags.
func (c *Controller) List() ([]PluginState, error) {
	discovered, err := c.discover()
	if err != nil {
		return nil, err
	}
	enabled := c.enabledSet()
	healthy := make(map[string]bool)
	for _, info := range c.reg.Infos() {
		healthy[info.ID] = true
	}
	out := make([]PluginState, 0, len(discovered))
	for _, d := range discovered {
		out = append(out, PluginState{
			ID:           d.ID,
			Capabilities: d.Capabilities,
			Enabled:      enabled[d.ID],
			Healthy:      healthy[d.ID],
			AuthProvider: d.HasCapability(plugin.CapAuthProvider),
		})
	}
	return out, nil
}

// SetEnabled toggles a plugin. Enablement is restart-to-apply for every plugin:
// the new state is persisted and takes effect on the next server boot. Always
// reports AppliedRestart.
func (c *Controller) SetEnabled(ctx context.Context, id string, enable bool) (Applied, error) {
	_, ok, err := c.findDescriptor(id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownPlugin, id)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.persist(ctx, id, enable); err != nil {
		return "", err
	}
	return AppliedRestart, nil
}

// persist writes the active-state into the plugin table. Enabling ensures the
// row exists, stamps installed_at when first installed, and sets active=true;
// disabling sets active=false (a no-op for a plugin that was never installed).
func (c *Controller) persist(ctx context.Context, id string, enable bool) error {
	if c.plugins == nil {
		return fmt.Errorf("pluginsctl: plugin repo unavailable, cannot persist enable-state")
	}
	existing, err := c.plugins.Get(ctx, id)
	if err != nil && !repo.IsNotFound(err) {
		return err
	}
	if !enable {
		if existing == nil {
			return nil
		}
		return c.plugins.SetActive(ctx, id, false)
	}
	if _, err := c.plugins.Upsert(ctx, repo.UpsertPluginInput{ID: id}); err != nil {
		return err
	}
	if existing == nil || existing.InstalledAt == nil {
		now := time.Now()
		if err := c.plugins.SetInstalledAt(ctx, id, &now); err != nil {
			return err
		}
	}
	return c.plugins.SetActive(ctx, id, true)
}

func (c *Controller) enabledSet() map[string]bool {
	set := make(map[string]bool)
	if c.plugins == nil {
		return set
	}
	rows, err := c.plugins.List(context.Background())
	if err != nil {
		return set
	}
	for _, p := range rows {
		if p.Active {
			set[p.ID] = true
		}
	}
	return set
}

func (c *Controller) findDescriptor(id string) (plugin.Descriptor, bool, error) {
	discovered, err := c.discover()
	if err != nil {
		return plugin.Descriptor{}, false, err
	}
	for _, d := range discovered {
		if d.ID == id {
			return d, true, nil
		}
	}
	return plugin.Descriptor{}, false, nil
}

// discover scans the plugin dir for subdirectories holding a valid plugin.json.
func (c *Controller) discover() ([]plugin.Descriptor, error) {
	if c.dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("pluginsctl: read dir %s: %w", c.dir, err)
	}
	var out []plugin.Descriptor
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, entry.Name(), "plugin.json"))
		if err != nil {
			continue
		}
		var desc plugin.Descriptor
		if err := json.Unmarshal(data, &desc); err != nil {
			continue
		}
		if desc.ID == "" {
			continue
		}
		out = append(out, desc)
	}
	return out, nil
}
