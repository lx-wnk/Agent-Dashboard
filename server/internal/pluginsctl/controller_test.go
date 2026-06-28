package pluginsctl_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
)

// fakeRegistry reports a fixed healthy set for listing.
type fakeRegistry struct {
	healthy []plugin.Info
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
