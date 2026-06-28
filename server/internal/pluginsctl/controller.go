// Package pluginsctl is the control plane for plugin enable/disable. It bridges
// the live plugin Registry (start/stop) and the persisted enable-list in
// settings, applying changes effect-first so a failed live start never persists.
package pluginsctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// enabledKey is the settings key holding the comma-joined list of enabled plugin IDs.
const enabledKey = "plugins.enabled"

// ErrUnknownPlugin signals that an ID matched no discovered plugin. The handler
// maps it to 400; any other error is a live start/stop failure mapped to 500.
var ErrUnknownPlugin = errors.New("pluginsctl: unknown plugin")

// Applied describes whether a change took effect immediately or needs a restart.
type Applied string

const (
	AppliedLive    Applied = "live"
	AppliedRestart Applied = "restart"
)

// Registry is the minimal subset of *plugin.Registry the controller needs,
// declared locally so tests can fake it.
type Registry interface {
	StartOne(ctx context.Context, id string) error
	StopOne(id string) error
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
	reg      Registry
	settings *settings.Service
	dir      string
	// mu serializes the read-modify-write of plugins.enabled in SetEnabled so
	// concurrent toggles cannot lose an update.
	mu sync.Mutex
}

// New builds a Controller. settings may be nil (no DB); in that case nothing is
// reported enabled and SetEnabled fails because the state cannot be persisted.
func New(reg Registry, svc *settings.Service, dir string) *Controller {
	return &Controller{reg: reg, settings: svc, dir: dir}
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

// SetEnabled toggles a plugin. Effect-first ordering: for non-auth plugins the
// live start/stop runs before persisting, so a failed start is not recorded as
// enabled. auth_provider plugins are wired at boot only, so they persist without
// a live effect and report AppliedRestart.
func (c *Controller) SetEnabled(ctx context.Context, id string, enable bool) (Applied, error) {
	desc, ok, err := c.findDescriptor(id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownPlugin, id)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if desc.HasCapability(plugin.CapAuthProvider) {
		if err := c.persist(ctx, id, enable); err != nil {
			return "", err
		}
		return AppliedRestart, nil
	}

	if enable {
		if err := c.reg.StartOne(ctx, id); err != nil {
			return "", fmt.Errorf("pluginsctl: start %q: %w", id, err)
		}
	} else if err := c.reg.StopOne(id); err != nil {
		return "", fmt.Errorf("pluginsctl: stop %q: %w", id, err)
	}
	if err := c.persist(ctx, id, enable); err != nil {
		return "", err
	}
	return AppliedLive, nil
}

// persist updates plugins.enabled to add or remove id, preserving order.
func (c *Controller) persist(ctx context.Context, id string, enable bool) error {
	if c.settings == nil {
		return fmt.Errorf("pluginsctl: settings unavailable, cannot persist enable-state")
	}
	current := c.settings.StringSlice(enabledKey)
	next := make([]string, 0, len(current)+1)
	seen := false
	for _, e := range current {
		if e == id {
			seen = true
			if enable {
				next = append(next, e)
			}
			continue
		}
		next = append(next, e)
	}
	if enable && !seen {
		next = append(next, id)
	}
	return c.settings.Set(ctx, enabledKey, strings.Join(next, ","))
}

func (c *Controller) enabledSet() map[string]bool {
	set := make(map[string]bool)
	if c.settings == nil {
		return set
	}
	for _, id := range c.settings.StringSlice(enabledKey) {
		set[id] = true
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
