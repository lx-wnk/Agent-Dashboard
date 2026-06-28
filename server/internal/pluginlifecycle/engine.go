// Package pluginlifecycle drives plugin state transitions (install/activate/
// deactivate/uninstall/update), persisting state and invoking declared HTTP
// hooks. Process orchestration (start/stop, reachability) is SP2.
package pluginlifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// State is a plugin's persisted lifecycle state.
type State struct {
	InstalledAt *time.Time
	Active      bool
	Version     string
}

// StateRepo is the subset of the plugin repo the engine needs.
type StateRepo interface {
	GetState(ctx context.Context, id string) (State, error)
	SetInstalledAt(ctx context.Context, id string, at *time.Time) error
	SetActive(ctx context.Context, id string, active bool) error
	SetVersion(ctx context.Context, id, version string) error
}

// HookCaller POSTs a lifecycle hook to a plugin. hook is the path (may be empty
// = no-op). The real impl is HTTP; SP2 ensures reachability.
type HookCaller interface {
	Call(ctx context.Context, d plugin.Descriptor, hook string) error
}

// SettingsClearer removes a plugin's settings on uninstall.
type SettingsClearer interface {
	Clear(ctx context.Context, pluginID string) error
}

type Engine struct {
	repo     StateRepo
	hooks    HookCaller
	settings SettingsClearer
}

func New(repo StateRepo, hooks HookCaller, settings SettingsClearer) *Engine {
	return &Engine{repo: repo, hooks: hooks, settings: settings}
}

// callHook runs a hook only when its path is non-empty.
func (e *Engine) callHook(ctx context.Context, d plugin.Descriptor, path string) error {
	if path == "" {
		return nil
	}
	return e.hooks.Call(ctx, d, path)
}

func (e *Engine) Install(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.InstalledAt != nil {
		return fmt.Errorf("pluginlifecycle: %s already installed", d.ID)
	}
	if err := e.callHook(ctx, d, d.Lifecycle.Install); err != nil {
		return fmt.Errorf("install hook: %w", err)
	}
	if err := e.callHook(ctx, d, d.Lifecycle.PostInstall); err != nil {
		return fmt.Errorf("postInstall hook: %w", err)
	}
	now := time.Now()
	return e.repo.SetInstalledAt(ctx, d.ID, &now)
}

func (e *Engine) Activate(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.InstalledAt == nil {
		return fmt.Errorf("pluginlifecycle: %s must be installed before activate", d.ID)
	}
	if err := e.callHook(ctx, d, d.Lifecycle.Activate); err != nil {
		return fmt.Errorf("activate hook: %w", err)
	}
	return e.repo.SetActive(ctx, d.ID, true)
}

func (e *Engine) Deactivate(ctx context.Context, d plugin.Descriptor) error {
	if err := e.callHook(ctx, d, d.Lifecycle.Deactivate); err != nil {
		return fmt.Errorf("deactivate hook: %w", err)
	}
	return e.repo.SetActive(ctx, d.ID, false)
}

func (e *Engine) Update(ctx context.Context, d plugin.Descriptor) error {
	if err := e.callHook(ctx, d, d.Lifecycle.Update); err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	return e.repo.SetVersion(ctx, d.ID, d.Version)
}

func (e *Engine) Uninstall(ctx context.Context, d plugin.Descriptor) error {
	st, err := e.repo.GetState(ctx, d.ID)
	if err != nil {
		return err
	}
	if st.Active {
		if err := e.Deactivate(ctx, d); err != nil {
			return err
		}
	}
	if err := e.callHook(ctx, d, d.Lifecycle.Uninstall); err != nil {
		return fmt.Errorf("uninstall hook: %w", err)
	}
	if err := e.repo.SetInstalledAt(ctx, d.ID, nil); err != nil {
		return err
	}
	return e.settings.Clear(ctx, d.ID)
}
