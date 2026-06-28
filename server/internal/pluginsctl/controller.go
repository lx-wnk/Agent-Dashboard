// Package pluginsctl is the control plane for listing discovered plugins.
// The registry is read-only here (health/info for listing); enable/disable is
// handled live by the lifecycle engine.
package pluginsctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// ErrUnknownPlugin signals that an ID matched no discovered plugin. The handler
// maps it to 400; any other error is a persistence failure mapped to 500.
var ErrUnknownPlugin = errors.New("pluginsctl: unknown plugin")

// ErrInvalidAction signals an unsupported lifecycle action (not one of
// install|activate|deactivate|uninstall). The handler maps it to 400.
var ErrInvalidAction = errors.New("pluginsctl: invalid action")

// Registry is the minimal subset of *plugin.Registry the controller needs,
// declared locally so tests can fake it.
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

// Controller resolves discovered plugins and reports their runtime state.
type Controller struct {
	reg     Registry
	plugins repo.PluginRepo
	dir     string
}

// New builds a Controller. plugins may be nil (no DB); in that case nothing is
// reported enabled.
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
