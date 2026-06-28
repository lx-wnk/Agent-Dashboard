package pluginlifecycle

import (
	"context"
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
	e := New(pr, hk, settings)
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
	e := New(&fakePluginRepo{}, &recordingHooks{}, &fakeClearer{})
	require.Error(t, e.Activate(context.Background(), desc()))
}

func TestEngine_HookFailureAbortsTransition(t *testing.T) {
	pr := &fakePluginRepo{}
	e := New(pr, &recordingHooks{failOn: "/i"}, &fakeClearer{})
	require.Error(t, e.Install(context.Background(), desc()))
	assert.Nil(t, pr.installedAt) // state not changed when hook fails
}

type fakeClearer struct{ cleared bool }

func (f *fakeClearer) Clear(_ context.Context, _ string) error { f.cleared = true; return nil }
