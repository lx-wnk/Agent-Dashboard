package pluginsctl_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
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

// newPluginRepo opens an in-memory DB and returns a real PluginRepo so tests
// exercise the actual table-backed persistence.
func newPluginRepo(t *testing.T) repo.PluginRepo {
	t.Helper()
	bundle, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewPluginRepo(bundle.Client)
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
	pr := newPluginRepo(t)
	ctl := pluginsctl.New(reg, pr, dir)

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
	got, err := pr.Get(context.Background(), "voice-whisper")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Active {
		t.Errorf("active = false, want true")
	}
	if got.InstalledAt == nil {
		t.Errorf("installed_at = nil, want set")
	}
}

func TestSetEnabled_DisableNonAuth_PersistsRestartNoStartStop(t *testing.T) {
	dir := t.TempDir()
	writeDescriptor(t, dir, plugin.Descriptor{ID: "voice-whisper", Capabilities: []string{plugin.CapRouteExtension}})
	reg := &fakeRegistry{}
	pr := newPluginRepo(t)
	ctx := context.Background()
	if _, err := pr.Upsert(ctx, repo.UpsertPluginInput{ID: "voice-whisper"}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	now := time.Now()
	if err := pr.SetInstalledAt(ctx, "voice-whisper", &now); err != nil {
		t.Fatalf("seed installed_at: %v", err)
	}
	if err := pr.SetActive(ctx, "voice-whisper", true); err != nil {
		t.Fatalf("seed active: %v", err)
	}
	ctl := pluginsctl.New(reg, pr, dir)

	applied, err := ctl.SetEnabled(ctx, "voice-whisper", false)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if applied != pluginsctl.AppliedRestart {
		t.Errorf("applied = %q, want restart", applied)
	}
	if len(reg.started) != 0 || len(reg.stopped) != 0 {
		t.Errorf("restart-to-apply must not start/stop live: started=%v stopped=%v", reg.started, reg.stopped)
	}
	got, err := pr.Get(ctx, "voice-whisper")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Active {
		t.Errorf("active = true, want false")
	}
}

func TestSetEnabled_AuthProvider_PersistsRestartNoStartStop(t *testing.T) {
	dir := t.TempDir()
	writeDescriptor(t, dir, plugin.Descriptor{ID: "github-auth", Capabilities: []string{plugin.CapAuthProvider}})
	reg := &fakeRegistry{}
	pr := newPluginRepo(t)
	ctl := pluginsctl.New(reg, pr, dir)

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
	got, err := pr.Get(context.Background(), "github-auth")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Active {
		t.Errorf("active = false, want true")
	}
}

func TestSetEnabled_UnknownID(t *testing.T) {
	dir := t.TempDir()
	ctl := pluginsctl.New(&fakeRegistry{}, newPluginRepo(t), dir)
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
	pr := newPluginRepo(t)
	ctx := context.Background()
	if _, err := pr.Upsert(ctx, repo.UpsertPluginInput{ID: "voice-whisper"}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	if err := pr.SetActive(ctx, "voice-whisper", true); err != nil {
		t.Fatalf("seed active: %v", err)
	}
	ctl := pluginsctl.New(reg, pr, dir)

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
