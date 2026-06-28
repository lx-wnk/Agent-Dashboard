package pluginsctl_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
	"github.com/lx-wnk/agent-dashboard/server/internal/settings"
)

// fakeRegistry records start/stop calls so tests can assert the controller never
// invokes the live path, and reports a fixed healthy set for listing.
type fakeRegistry struct {
	started []string
	stopped []string
	healthy []plugin.Info
}

func (f *fakeRegistry) StartOne(_ context.Context, id string) error {
	f.started = append(f.started, id)
	return nil
}

func (f *fakeRegistry) StopOne(id string) error {
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeRegistry) Infos() []plugin.Info { return f.healthy }

// fakeRepo is an in-memory settings.Repo.
type fakeRepo struct{ m map[string]string }

func (f *fakeRepo) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := f.m[k]
	return v, ok, nil
}
func (f *fakeRepo) Set(_ context.Context, k, v string) error { f.m[k] = v; return nil }
func (f *fakeRepo) ListAll(_ context.Context) (map[string]string, error) {
	return f.m, nil
}

func newSettings(t *testing.T) *settings.Service {
	t.Helper()
	svc := settings.New(&fakeRepo{m: map[string]string{}})
	if err := svc.Load(context.Background()); err != nil {
		t.Fatalf("settings load: %v", err)
	}
	return svc
}

// writeDescriptor writes a plugin.json into dir/<id>/.
func writeDescriptor(t *testing.T, dir string, desc plugin.Descriptor) {
	t.Helper()
	sub := filepath.Join(dir, desc.ID)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(desc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "plugin.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSetEnabled_EnableNonAuth_PersistsRestartNoStartStop(t *testing.T) {
	dir := t.TempDir()
	writeDescriptor(t, dir, plugin.Descriptor{ID: "voice-whisper", Capabilities: []string{plugin.CapRouteExtension}})
	reg := &fakeRegistry{}
	svc := newSettings(t)
	ctl := pluginsctl.New(reg, svc, dir)

	applied, err := ctl.SetEnabled(context.Background(), "voice-whisper", true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if applied != pluginsctl.AppliedRestart {
		t.Errorf("applied = %q, want restart", applied)
	}
	if len(reg.started) != 0 || len(reg.stopped) != 0 {
		t.Errorf("restart-to-apply must not start/stop live: started=%v stopped=%v", reg.started, reg.stopped)
	}
	if got := svc.StringSlice("plugins.enabled"); len(got) != 1 || got[0] != "voice-whisper" {
		t.Errorf("plugins.enabled = %v, want [voice-whisper]", got)
	}
}

func TestSetEnabled_DisableNonAuth_PersistsRestartNoStartStop(t *testing.T) {
	dir := t.TempDir()
	writeDescriptor(t, dir, plugin.Descriptor{ID: "voice-whisper", Capabilities: []string{plugin.CapRouteExtension}})
	reg := &fakeRegistry{}
	svc := newSettings(t)
	if err := svc.Set(context.Background(), "plugins.enabled", "voice-whisper"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctl := pluginsctl.New(reg, svc, dir)

	applied, err := ctl.SetEnabled(context.Background(), "voice-whisper", false)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if applied != pluginsctl.AppliedRestart {
		t.Errorf("applied = %q, want restart", applied)
	}
	if len(reg.started) != 0 || len(reg.stopped) != 0 {
		t.Errorf("restart-to-apply must not start/stop live: started=%v stopped=%v", reg.started, reg.stopped)
	}
	if got := svc.StringSlice("plugins.enabled"); len(got) != 0 {
		t.Errorf("plugins.enabled = %v, want empty", got)
	}
}

func TestSetEnabled_AuthProvider_PersistsRestartNoStartStop(t *testing.T) {
	dir := t.TempDir()
	writeDescriptor(t, dir, plugin.Descriptor{ID: "github-auth", Capabilities: []string{plugin.CapAuthProvider}})
	reg := &fakeRegistry{}
	svc := newSettings(t)
	ctl := pluginsctl.New(reg, svc, dir)

	applied, err := ctl.SetEnabled(context.Background(), "github-auth", true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if applied != pluginsctl.AppliedRestart {
		t.Errorf("applied = %q, want restart", applied)
	}
	if len(reg.started) != 0 || len(reg.stopped) != 0 {
		t.Errorf("auth_provider must not start/stop live: started=%v stopped=%v", reg.started, reg.stopped)
	}
	if got := svc.StringSlice("plugins.enabled"); len(got) != 1 || got[0] != "github-auth" {
		t.Errorf("plugins.enabled = %v, want [github-auth]", got)
	}
}

func TestSetEnabled_UnknownID(t *testing.T) {
	dir := t.TempDir()
	ctl := pluginsctl.New(&fakeRegistry{}, newSettings(t), dir)
	_, err := ctl.SetEnabled(context.Background(), "does-not-exist", true)
	if err == nil {
		t.Fatal("expected ErrUnknownPlugin")
	}
	if !errors.Is(err, pluginsctl.ErrUnknownPlugin) {
		t.Errorf("error %v is not ErrUnknownPlugin", err)
	}
}

func TestList_FlagsReflectDiscoveryEnabledAndHealth(t *testing.T) {
	dir := t.TempDir()
	writeDescriptor(t, dir, plugin.Descriptor{ID: "voice-whisper", Capabilities: []string{plugin.CapRouteExtension}})
	writeDescriptor(t, dir, plugin.Descriptor{ID: "github-auth", Capabilities: []string{plugin.CapAuthProvider}})
	reg := &fakeRegistry{healthy: []plugin.Info{{ID: "voice-whisper"}}}
	svc := newSettings(t)
	if err := svc.Set(context.Background(), "plugins.enabled", "voice-whisper"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctl := pluginsctl.New(reg, svc, dir)

	states, err := ctl.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]pluginsctl.PluginState{}
	for _, s := range states {
		byID[s.ID] = s
	}
	if len(byID) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(byID))
	}
	whisper := byID["voice-whisper"]
	if !whisper.Enabled || !whisper.Healthy || whisper.AuthProvider {
		t.Errorf("voice-whisper flags wrong: %+v", whisper)
	}
	auth := byID["github-auth"]
	if auth.Enabled || auth.Healthy || !auth.AuthProvider {
		t.Errorf("github-auth flags wrong (disabled, unhealthy, authProvider): %+v", auth)
	}
}
