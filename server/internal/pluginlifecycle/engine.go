// Package pluginlifecycle drives plugin state transitions (install/activate/
// deactivate/uninstall/update), persisting state and invoking declared HTTP
// hooks. Process orchestration (start/stop, reachability) is SP2.
package pluginlifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// ErrIllegalTransition is returned when a lifecycle action cannot be applied in
// the plugin's current state (e.g. install-when-already-installed).
var ErrIllegalTransition = errors.New("pluginlifecycle: illegal transition")

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

// ProcessManager runs plugin processes for the engine. The registry implements
// it (via an adapter); a nil manager makes every method a no-op so the engine
// is testable in isolation.
type ProcessManager interface {
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	WithTransient(ctx context.Context, id string, fn func() error) error
}

type Engine struct {
	repo     StateRepo
	hooks    HookCaller
	settings SettingsClearer
	proc     ProcessManager
}

func New(repo StateRepo, hooks HookCaller, settings SettingsClearer, proc ProcessManager) *Engine {
	return &Engine{repo: repo, hooks: hooks, settings: settings, proc: proc}
}

func (e *Engine) start(ctx context.Context, id string) error {
	if e.proc == nil {
		return nil
	}
	return e.proc.Start(ctx, id)
}

func (e *Engine) stop(ctx context.Context, id string) error {
	if e.proc == nil {
		return nil
	}
	return e.proc.Stop(ctx, id)
}

func (e *Engine) withTransient(ctx context.Context, id string, fn func() error) error {
	if e.proc == nil {
		return fn()
	}
	return e.proc.WithTransient(ctx, id, fn)
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
		return fmt.Errorf("%w: %s already installed", ErrIllegalTransition, d.ID)
	}
	if err := e.withTransient(ctx, d.ID, func() error {
		if err := e.callHook(ctx, d, d.Lifecycle.Install); err != nil {
			return fmt.Errorf("install hook: %w", err)
		}
		return e.callHook(ctx, d, d.Lifecycle.PostInstall)
	}); err != nil {
		return err
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
		return fmt.Errorf("%w: %s must be installed before activate", ErrIllegalTransition, d.ID)
	}
	if err := e.start(ctx, d.ID); err != nil {
		return fmt.Errorf("activate start: %w", err)
	}
	if err := e.callHook(ctx, d, d.Lifecycle.Activate); err != nil {
		_ = e.stop(ctx, d.ID)
		return fmt.Errorf("activate hook: %w", err)
	}
	if err := e.repo.SetActive(ctx, d.ID, true); err != nil {
		_ = e.stop(ctx, d.ID)
		return err
	}
	return nil
}

func (e *Engine) Deactivate(ctx context.Context, d plugin.Descriptor) error {
	if err := e.callHook(ctx, d, d.Lifecycle.Deactivate); err != nil {
		return fmt.Errorf("deactivate hook: %w", err)
	}
	if err := e.repo.SetActive(ctx, d.ID, false); err != nil {
		return err
	}
	return e.stop(ctx, d.ID)
}

func (e *Engine) Update(ctx context.Context, d plugin.Descriptor) error {
	if err := e.withTransient(ctx, d.ID, func() error {
		return e.callHook(ctx, d, d.Lifecycle.Update)
	}); err != nil {
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
	if err := e.withTransient(ctx, d.ID, func() error {
		return e.callHook(ctx, d, d.Lifecycle.Uninstall)
	}); err != nil {
		return fmt.Errorf("uninstall hook: %w", err)
	}
	if err := e.repo.SetInstalledAt(ctx, d.ID, nil); err != nil {
		return err
	}
	return e.settings.Clear(ctx, d.ID)
}
