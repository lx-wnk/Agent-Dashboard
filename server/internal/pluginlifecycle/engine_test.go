package pluginlifecycle

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

type fakePluginRepo struct {
	installedAt *time.Time
	active      bool
	version     string
}

func (f *fakePluginRepo) GetState(_ context.Context, _ string) (State, error) {
	return State{InstalledAt: f.installedAt, Active: f.active, Version: f.version}, nil
}
func (f *fakePluginRepo) SetInstalledAt(_ context.Context, _ string, at *time.Time) error {
	f.installedAt = at
	return nil
}
func (f *fakePluginRepo) SetActive(_ context.Context, _ string, a bool) error {
	f.active = a
	return nil
}
func (f *fakePluginRepo) SetVersion(_ context.Context, _ string, v string) error {
	f.version = v
	return nil
}

type recordingHooks struct {
	called []string
	failOn string
}

func (r *recordingHooks) Call(_ context.Context, _ plugin.Descriptor, hook string) error {
	r.called = append(r.called, hook)
	if hook == r.failOn {
		return assertErr
	}
	return nil
}

var assertErr = &hookErr{}

type hookErr struct{}

func (*hookErr) Error() string { return "hook failed" }

func desc() plugin.Descriptor {
	return plugin.Descriptor{ID: "p1", Version: "1.0.0",
		Lifecycle: plugin.LifecycleHooks{Install: "/i", Activate: "/a", Deactivate: "/d", Uninstall: "/u"}}
}

func TestEngine_InstallActivateDeactivateUninstall(t *testing.T) {
	pr := &fakePluginRepo{}
	hk := &recordingHooks{}
	settings := &fakeClearer{}
	e := New(pr, hk, settings, nil)
	ctx := context.Background()
	d := desc()

	require.NoError(t, e.Install(ctx, d))
	assert.NotNil(t, pr.installedAt)
	assert.Contains(t, hk.called, "/i")

	require.NoError(t, e.Activate(ctx, d))
	assert.True(t, pr.active)
	assert.Contains(t, hk.called, "/a")

	require.NoError(t, e.Deactivate(ctx, d))
	assert.False(t, pr.active)

	require.NoError(t, e.Uninstall(ctx, d))
	assert.Nil(t, pr.installedAt)
	assert.True(t, settings.cleared)
}

func TestEngine_ActivateBeforeInstallRejected(t *testing.T) {
	e := New(&fakePluginRepo{}, &recordingHooks{}, &fakeClearer{}, nil)
	err := e.Activate(context.Background(), desc())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrIllegalTransition)
}

func TestEngine_InstallWhenAlreadyInstalledRejected(t *testing.T) {
	now := time.Now()
	e := New(&fakePluginRepo{installedAt: &now}, &recordingHooks{}, &fakeClearer{}, nil)
	err := e.Install(context.Background(), desc())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrIllegalTransition)
}

func TestEngine_HookFailureAbortsTransition(t *testing.T) {
	pr := &fakePluginRepo{}
	e := New(pr, &recordingHooks{failOn: "/i"}, &fakeClearer{}, nil)
	require.Error(t, e.Install(context.Background(), desc()))
	assert.Nil(t, pr.installedAt) // state not changed when hook fails
}

type fakeClearer struct{ cleared bool }

func (f *fakeClearer) Clear(_ context.Context, _ string) error { f.cleared = true; return nil }

// --- ordering / process-manager tests ---

// eventRepo embeds fakePluginRepo and records SetActive calls to a shared
// events slice so call order is assertable across proc + hook + repo.
type eventRepo struct {
	fakePluginRepo
	events *[]string
}

func newInstalledRepo(events *[]string) *eventRepo {
	r := &eventRepo{events: events}
	now := time.Now()
	r.installedAt = &now
	return r
}

func (r *eventRepo) SetActive(_ context.Context, _ string, active bool) error {
	*r.events = append(*r.events, fmt.Sprintf("setActive:%v", active))
	r.fakePluginRepo.active = active
	return nil
}

// eventHooks records "hook:"+path to a shared events slice.
type eventHooks struct {
	events *[]string
	fail   bool
}

func (h *eventHooks) Call(_ context.Context, _ plugin.Descriptor, path string) error {
	*h.events = append(*h.events, "hook:"+path)
	if h.fail {
		return fmt.Errorf("hook failed")
	}
	return nil
}

// fakeProc records start/stop/transient events to a shared events slice.
type fakeProc struct{ events *[]string }

func (f fakeProc) Start(_ context.Context, id string) error {
	*f.events = append(*f.events, "start:"+id)
	return nil
}
func (f fakeProc) Stop(_ context.Context, id string) error {
	*f.events = append(*f.events, "stop:"+id)
	return nil
}
func (f fakeProc) WithTransient(_ context.Context, id string, fn func() error) error {
	*f.events = append(*f.events, "transient-begin:"+id)
	err := fn()
	*f.events = append(*f.events, "transient-end:"+id)
	return err
}

func TestActivateStartsBeforeHookThenSetsActive(t *testing.T) {
	var events []string
	repo := newInstalledRepo(&events)
	hooks := &eventHooks{events: &events}
	eng := New(repo, hooks, &fakeClearer{}, fakeProc{events: &events})

	d := plugin.Descriptor{ID: "voice", Lifecycle: plugin.LifecycleHooks{Activate: "/lifecycle/activate"}}
	require.NoError(t, eng.Activate(context.Background(), d))

	require.Equal(t, []string{"start:voice", "hook:/lifecycle/activate", "setActive:true"}, events)
}

func TestActivateHookFailureStopsAndDoesNotActivate(t *testing.T) {
	var events []string
	repo := newInstalledRepo(&events)
	hooks := &eventHooks{events: &events, fail: true}
	eng := New(repo, hooks, &fakeClearer{}, fakeProc{events: &events})

	d := plugin.Descriptor{ID: "voice", Lifecycle: plugin.LifecycleHooks{Activate: "/lifecycle/activate"}}
	require.Error(t, eng.Activate(context.Background(), d))
	require.Equal(t, []string{"start:voice", "hook:/lifecycle/activate", "stop:voice"}, events)
	require.False(t, repo.active)
}

func TestInstallWrapsHooksInTransient(t *testing.T) {
	var events []string
	repo := &eventRepo{events: &events}
	hooks := &eventHooks{events: &events}
	eng := New(repo, hooks, &fakeClearer{}, fakeProc{events: &events})

	d := plugin.Descriptor{ID: "voice", Lifecycle: plugin.LifecycleHooks{Install: "/lifecycle/install"}}
	require.NoError(t, eng.Install(context.Background(), d))

	require.Equal(t, "transient-begin:voice", events[0])
	require.Equal(t, "transient-end:voice", events[len(events)-1])
}
