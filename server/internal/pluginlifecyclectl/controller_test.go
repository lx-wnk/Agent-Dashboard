package pluginlifecyclectl_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginlifecyclectl"
	"github.com/lx-wnk/agent-dashboard/server/internal/pluginsctl"
)

type fakeRepo struct {
	rows map[string]*ent.Plugin
	list []*ent.Plugin
}

func (f *fakeRepo) List(context.Context) ([]*ent.Plugin, error) { return f.list, nil }

func (f *fakeRepo) Get(_ context.Context, id string) (*ent.Plugin, error) {
	if p, ok := f.rows[id]; ok {
		return p, nil
	}
	return nil, &ent.NotFoundError{}
}

type fakeEngine struct {
	calls []string
}

func (f *fakeEngine) Install(_ context.Context, d plugin.Descriptor) error {
	f.calls = append(f.calls, "install:"+d.ID)
	return nil
}
func (f *fakeEngine) Activate(_ context.Context, d plugin.Descriptor) error {
	f.calls = append(f.calls, "activate:"+d.ID)
	return nil
}
func (f *fakeEngine) Deactivate(_ context.Context, d plugin.Descriptor) error {
	f.calls = append(f.calls, "deactivate:"+d.ID)
	return nil
}
func (f *fakeEngine) Uninstall(_ context.Context, d plugin.Descriptor) error {
	f.calls = append(f.calls, "uninstall:"+d.ID)
	return nil
}
func (f *fakeEngine) Update(_ context.Context, d plugin.Descriptor, hash string) error {
	f.calls = append(f.calls, "update:"+d.ID+":"+hash)
	return nil
}

type fakeSettings struct {
	getSchema []plugin.SettingField
	putSchema []plugin.SettingField
	putID     string
	putValues map[string]string
}

func (f *fakeSettings) Get(_ context.Context, _ string, schema []plugin.SettingField) (map[string]string, error) {
	f.getSchema = schema
	return map[string]string{"k": "v"}, nil
}

func (f *fakeSettings) Put(_ context.Context, id string, schema []plugin.SettingField, values map[string]string) error {
	f.putID, f.putSchema, f.putValues = id, schema, values
	return nil
}

type fakeLoader struct {
	manifests map[string]plugin.Descriptor
	hashes    map[string]string
}

func (f *fakeLoader) Load(id, _ string) (plugin.Descriptor, string, error) {
	d, ok := f.manifests[id]
	if !ok {
		return plugin.Descriptor{}, "", errors.New("no manifest")
	}
	return d, f.hashes[id], nil
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestList_DerivesStateAndFlags(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{list: []*ent.Plugin{
		{ID: "disc", ManifestHash: "h-disc"},
		{ID: "inact", InstalledAt: ptrTime(now), Active: false, ManifestHash: "h-old"},
		{ID: "act", InstalledAt: ptrTime(now), Active: true, ManifestHash: "h-act"},
	}}
	loader := &fakeLoader{
		manifests: map[string]plugin.Descriptor{
			"disc":  {Capabilities: []string{"route_extension"}},
			"inact": {Settings: []plugin.SettingField{{Key: "token", Secret: true}}},
			"act":   {},
		},
		hashes: map[string]string{"disc": "h-disc", "inact": "h-new", "act": "h-act"},
	}
	c := pluginlifecyclectl.NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader)

	views, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("expected 3 views, got %d", len(views))
	}
	byID := map[string]struct {
		state string
		upd   bool
		hasS  bool
	}{}
	for _, v := range views {
		byID[v.ID] = struct {
			state string
			upd   bool
			hasS  bool
		}{v.State, v.UpdateAvailable, v.HasSettings}
	}
	if byID["disc"].state != "discovered" {
		t.Errorf("disc state: got %q", byID["disc"].state)
	}
	if byID["inact"].state != "inactive" {
		t.Errorf("inact state: got %q", byID["inact"].state)
	}
	if byID["act"].state != "active" {
		t.Errorf("act state: got %q", byID["act"].state)
	}
	if byID["inact"].upd != true {
		t.Errorf("inact should have update available (hash mismatch)")
	}
	if byID["act"].upd != false {
		t.Errorf("act should not have update available (hash match)")
	}
	if byID["inact"].hasS != true {
		t.Errorf("inact should report hasSettings")
	}
	if byID["disc"].hasS != false {
		t.Errorf("disc should not report hasSettings")
	}
}

func TestTransition_DispatchesAndReturnsState(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{rows: map[string]*ent.Plugin{
		"p1": {ID: "p1", Name: "P1", Version: "1.0", InstalledAt: ptrTime(now), Active: true, ManifestHash: "h"},
	}}
	engine := &fakeEngine{}
	loader := &fakeLoader{
		manifests: map[string]plugin.Descriptor{"p1": {ID: "p1", Capabilities: []string{"auth_provider"}}},
		hashes:    map[string]string{"p1": "h"},
	}
	c := pluginlifecyclectl.NewWithLoader(repo, engine, &fakeSettings{}, loader)

	view, err := c.Transition(context.Background(), "p1", "activate")
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if len(engine.calls) != 1 || engine.calls[0] != "activate:p1" {
		t.Errorf("engine calls: %v", engine.calls)
	}
	if view.State != "active" || view.ID != "p1" {
		t.Errorf("view wrong: %+v", view)
	}
}

func TestTransition_UpdateDispatchesToEngine(t *testing.T) {
	now := time.Now()
	// ManifestHash == loader hash → updateAvailable=false after update
	repo := &fakeRepo{rows: map[string]*ent.Plugin{
		"p1": {ID: "p1", Name: "P1", Version: "1.0", InstalledAt: ptrTime(now), ManifestHash: "h-new"},
	}}
	engine := &fakeEngine{}
	loader := &fakeLoader{
		manifests: map[string]plugin.Descriptor{"p1": {ID: "p1", Version: "2.0"}},
		hashes:    map[string]string{"p1": "h-new"},
	}
	c := pluginlifecyclectl.NewWithLoader(repo, engine, &fakeSettings{}, loader)

	view, err := c.Transition(context.Background(), "p1", "update")
	if err != nil {
		t.Fatalf("Transition update: %v", err)
	}
	if len(engine.calls) != 1 || engine.calls[0] != "update:p1:h-new" {
		t.Errorf("engine calls: %v", engine.calls)
	}
	if view.UpdateAvailable {
		t.Error("updateAvailable should be false after update (stored hash now matches manifest hash)")
	}
}

func TestTransition_InvalidAction(t *testing.T) {
	repo := &fakeRepo{rows: map[string]*ent.Plugin{"p1": {ID: "p1"}}}
	loader := &fakeLoader{manifests: map[string]plugin.Descriptor{"p1": {ID: "p1"}}, hashes: map[string]string{"p1": "h"}}
	c := pluginlifecyclectl.NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader)

	_, err := c.Transition(context.Background(), "p1", "frobnicate")
	if !errors.Is(err, pluginsctl.ErrInvalidAction) {
		t.Fatalf("expected ErrInvalidAction, got %v", err)
	}
}

func TestTransition_UnknownPlugin(t *testing.T) {
	repo := &fakeRepo{rows: map[string]*ent.Plugin{}}
	loader := &fakeLoader{manifests: map[string]plugin.Descriptor{}}
	c := pluginlifecyclectl.NewWithLoader(repo, &fakeEngine{}, &fakeSettings{}, loader)

	_, err := c.Transition(context.Background(), "ghost", "activate")
	if !errors.Is(err, pluginsctl.ErrUnknownPlugin) {
		t.Fatalf("expected ErrUnknownPlugin, got %v", err)
	}
}

func TestGetSettings_DelegatesWithSchema(t *testing.T) {
	schema := []plugin.SettingField{{Key: "token", Secret: true}}
	repo := &fakeRepo{rows: map[string]*ent.Plugin{"p1": {ID: "p1"}}}
	loader := &fakeLoader{manifests: map[string]plugin.Descriptor{"p1": {ID: "p1", Settings: schema}}, hashes: map[string]string{"p1": "h"}}
	settings := &fakeSettings{}
	c := pluginlifecyclectl.NewWithLoader(repo, &fakeEngine{}, settings, loader)

	gotSchema, values, err := c.GetSettings(context.Background(), "p1")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if len(gotSchema) != 1 || gotSchema[0].Key != "token" {
		t.Errorf("schema wrong: %v", gotSchema)
	}
	if len(settings.getSchema) != 1 {
		t.Errorf("service not called with manifest schema: %v", settings.getSchema)
	}
	if values["k"] != "v" {
		t.Errorf("values not delegated: %v", values)
	}
}

// slowEngine records start/end events with a configurable delay, used to verify
// per-plugin lock serialization.
type slowEngine struct {
	mu    sync.Mutex
	order []string
	delay time.Duration
}

func (e *slowEngine) record(s string) {
	e.mu.Lock()
	e.order = append(e.order, s)
	e.mu.Unlock()
}

func (e *slowEngine) Install(_ context.Context, d plugin.Descriptor) error {
	e.record("start:install:" + d.ID)
	time.Sleep(e.delay)
	e.record("end:install:" + d.ID)
	return nil
}
func (e *slowEngine) Activate(_ context.Context, d plugin.Descriptor) error {
	e.record("start:activate:" + d.ID)
	time.Sleep(e.delay)
	e.record("end:activate:" + d.ID)
	return nil
}
func (e *slowEngine) Deactivate(_ context.Context, d plugin.Descriptor) error {
	e.record("start:deactivate:" + d.ID)
	time.Sleep(e.delay)
	e.record("end:deactivate:" + d.ID)
	return nil
}
func (e *slowEngine) Uninstall(_ context.Context, d plugin.Descriptor) error {
	e.record("start:uninstall:" + d.ID)
	time.Sleep(e.delay)
	e.record("end:uninstall:" + d.ID)
	return nil
}
func (e *slowEngine) Update(_ context.Context, d plugin.Descriptor, _ string) error {
	e.record("start:update:" + d.ID)
	time.Sleep(e.delay)
	e.record("end:update:" + d.ID)
	return nil
}

func TestTransition_SamePluginSerializes(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{rows: map[string]*ent.Plugin{
		"p1": {ID: "p1", InstalledAt: ptrTime(now), Active: true, ManifestHash: "h"},
	}}
	eng := &slowEngine{delay: 50 * time.Millisecond}
	loader := &fakeLoader{
		manifests: map[string]plugin.Descriptor{"p1": {ID: "p1"}},
		hashes:    map[string]string{"p1": "h"},
	}
	c := pluginlifecyclectl.NewWithLoader(repo, eng, &fakeSettings{}, loader)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = c.Transition(context.Background(), "p1", "activate") }()
	// Small stagger so the first goroutine acquires the per-plugin lock before the second.
	time.Sleep(5 * time.Millisecond)
	go func() { defer wg.Done(); _, _ = c.Transition(context.Background(), "p1", "deactivate") }()
	wg.Wait()

	eng.mu.Lock()
	order := make([]string, len(eng.order))
	copy(order, eng.order)
	eng.mu.Unlock()

	if len(order) < 4 {
		t.Fatalf("expected 4 events, got %v", order)
	}
	// Serialized: end of first action must precede start of second.
	// Interleaved would look like: start:X, start:Y, end:X, end:Y.
	if !strings.HasPrefix(order[1], "end:") {
		t.Errorf("transitions interleaved: %v", order)
	}
}

func TestPutSettings_DelegatesWithSchema(t *testing.T) {
	schema := []plugin.SettingField{{Key: "endpoint"}}
	repo := &fakeRepo{rows: map[string]*ent.Plugin{"p1": {ID: "p1"}}}
	loader := &fakeLoader{manifests: map[string]plugin.Descriptor{"p1": {ID: "p1", Settings: schema}}, hashes: map[string]string{"p1": "h"}}
	settings := &fakeSettings{}
	c := pluginlifecyclectl.NewWithLoader(repo, &fakeEngine{}, settings, loader)

	err := c.PutSettings(context.Background(), "p1", map[string]string{"endpoint": "https://x"})
	if err != nil {
		t.Fatalf("PutSettings: %v", err)
	}
	if settings.putID != "p1" {
		t.Errorf("put id: %q", settings.putID)
	}
	if len(settings.putSchema) != 1 || settings.putSchema[0].Key != "endpoint" {
		t.Errorf("put schema wrong: %v", settings.putSchema)
	}
	if settings.putValues["endpoint"] != "https://x" {
		t.Errorf("put values wrong: %v", settings.putValues)
	}
}
