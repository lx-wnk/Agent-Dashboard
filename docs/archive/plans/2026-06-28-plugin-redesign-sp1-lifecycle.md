# Plugin Redesign SP1 — Lifecycle Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the plugin lifecycle backbone — a `plugin` DB model with full lifecycle (install/activate/deactivate/uninstall + update via HTTP hooks), per-plugin settings with encrypted secrets, manifest v2, discovery, a lifecycle API, and migration from #230's interim `plugins.enabled`.

**Architecture:** Plugins stay external subprocesses. The dashboard tracks each plugin's state (Shopware-style `installed_at`+`active`) in a `plugin` table, stores per-plugin settings in a `plugin_setting` table (secret fields AES-GCM-encrypted at rest with a bootstrapped master key), and drives state transitions through a lifecycle engine that POSTs declared hooks to the plugin over HTTP. Process start/stop and the catch-all live dispatcher are **SP2** — SP1 ships with a faked hook-caller and is testable in isolation.

**Tech Stack:** Go 1.26, ent ORM, modernc/sqlite, chi, crypto/aes+cipher; existing patterns from `provider_setting`/`app_setting` (#230).

**Spec:** `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md`

**Branch:** Implement on a branch off `feat/db-backed-settings` (PR #230) — SP1 reuses #230's settings registry + supersedes its `plugins.enabled`. If #230 has merged, branch off `main`. Name: `feat/plugin-sp1-lifecycle`.

**Conventions (every task):** `cd server && go build ./... && go test ./<touched pkg>/` before each commit (do NOT run `go test ./...` — it regenerates ent and can corrupt the working tree; if the ent tree drifts, `git checkout -- server/internal/db/ent/`). Commit `--no-gpg-sign`, English Conventional-Commit, NO task/phase numbers in messages. Don't stage `server/frontend/dist/.gitkeep`. Ignore stale gopls "undefined" diagnostics — `go build`/`go test` are authoritative.

---

## File Structure

**New:**
- `server/internal/plugin/manifest.go` — manifest v2 field types (SlotBinding, SettingField, LifecycleHooks) added to the `plugin` package (or extend `types.go`).
- `server/internal/db/ent/schema/plugin.go` — `plugin` ent schema.
- `server/internal/db/ent/schema/plugin_setting.go` — `plugin_setting` ent schema.
- `server/internal/db/repo/plugin_repo.go` — plugin state repo.
- `server/internal/db/repo/plugin_setting_repo.go` — plugin-setting repo.
- `server/internal/secretbox/secretbox.go` — AES-GCM encrypt/decrypt + master-key loader.
- `server/internal/secretbox/secretbox_test.go`
- `server/internal/pluginsettings/service.go` — per-plugin settings (schema+values, encrypt/mask/decrypt).
- `server/internal/pluginlifecycle/engine.go` — state machine + hook HTTP caller.
- `server/internal/pluginlifecycle/discovery.go` — directory scan → upsert.
- matching `_test.go` files.

**Modified:**
- `server/internal/plugin/types.go` — extend `Descriptor` with v2 fields.
- `server/internal/api/plugins/handler.go` — lifecycle + settings endpoints.
- `server/internal/api/router.go` — route wiring (RouterDeps).
- `server/cmd/serve/di.go` — construct repos/services/handler + master key; boot predicate reads `plugin` table.
- `server/internal/settings/registry.go` — remove the `plugins.enabled` key (superseded).

---

## Task 1: Manifest v2 — extend `Descriptor`

**Files:**
- Modify: `server/internal/plugin/types.go`
- Test: `server/internal/plugin/manifest_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package plugin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptor_ParsesV2(t *testing.T) {
	raw := `{
	  "id":"voice-whisper","name":"Voice (Whisper)","version":"1.2.0",
	  "capabilities":["route_extension"],"addr":"127.0.0.1:19010","command":["./voice-whisper"],
	  "slots":[{"slot":"agent-toolbar","priority":100,"mode":"extend"}],
	  "settings":[{"key":"endpoint","type":"url","label":"Endpoint"},
	              {"key":"apiKey","type":"string","label":"API Key","secret":true}],
	  "lifecycle":{"install":"/lifecycle/install","activate":"/lifecycle/activate"},
	  "permissions":["net"]
	}`
	var d Descriptor
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	assert.Equal(t, "Voice (Whisper)", d.Name)
	require.Len(t, d.Slots, 1)
	assert.Equal(t, "agent-toolbar", d.Slots[0].Slot)
	assert.Equal(t, 100, d.Slots[0].Priority)
	assert.Equal(t, "extend", d.Slots[0].Mode)
	require.Len(t, d.Settings, 2)
	assert.True(t, d.Settings[1].Secret)
	assert.Equal(t, "/lifecycle/activate", d.Lifecycle.Activate)
}

func TestDescriptor_BackwardCompatV1(t *testing.T) {
	// An old manifest with no v2 fields must still parse, with zero-value v2 fields.
	raw := `{"id":"old","capabilities":["auth_provider"],"addr":"127.0.0.1:9000","command":["./old"]}`
	var d Descriptor
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	assert.Equal(t, "old", d.ID)
	assert.Empty(t, d.Slots)
	assert.Empty(t, d.Settings)
	assert.Empty(t, d.Name)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/plugin/ -run 'TestDescriptor_ParsesV2|TestDescriptor_BackwardCompatV1' -v`
Expected: FAIL — `d.Name`/`d.Slots`/etc. undefined.

- [ ] **Step 3: Extend `Descriptor` in `types.go`**

Add fields to the existing `Descriptor` struct and new types:

```go
type Descriptor struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	Addr         string   `json:"addr"`
	Command      []string `json:"command"`
	Env          []string `json:"env"`
	Slots        []SlotBinding   `json:"slots"`
	Settings     []SettingField  `json:"settings"`
	Lifecycle    LifecycleHooks  `json:"lifecycle"`
	Permissions  []string        `json:"permissions"`
}

// SlotBinding declares that the plugin contributes UI into a named host slot.
// Mode is "override" (replace) or "extend" (wrap, receiving the parent). Higher
// Priority renders first. Consumed by the frontend (SP4).
type SlotBinding struct {
	Slot     string `json:"slot"`
	Priority int    `json:"priority"`
	Mode     string `json:"mode"`
}

// SettingField declares one configurable setting. Secret fields are encrypted at
// rest and masked in the API.
type SettingField struct {
	Key    string   `json:"key"`
	Type   string   `json:"type"`  // string|url|int|bool|enum
	Label  string   `json:"label"`
	Secret bool     `json:"secret"`
	Enum   []string `json:"enum,omitempty"`
}

// LifecycleHooks are optional HTTP paths (on the plugin's Addr) invoked on state
// transitions. An empty path means the transition runs without a hook.
type LifecycleHooks struct {
	Install     string `json:"install"`
	PostInstall string `json:"postInstall"`
	Activate    string `json:"activate"`
	Deactivate  string `json:"deactivate"`
	Update      string `json:"update"`
	Uninstall   string `json:"uninstall"`
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/plugin/ -v`
Expected: PASS (existing plugin tests still green — fields are additive).

- [ ] **Step 5: Commit**

```bash
git add server/internal/plugin/types.go server/internal/plugin/manifest_test.go
git commit --no-gpg-sign -m "feat: extend plugin manifest with v2 fields (slots, settings, lifecycle)"
```

---

## Task 2: `plugin` ent schema

**Files:**
- Create: `server/internal/db/ent/schema/plugin.go`

- [ ] **Step 1: Write the schema**

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Plugin persists discovered/installed/active state per plugin (Shopware-style:
// installed_at nullable + active bool). State is derived: no installed_at =
// discovered; set + active=false = inactive; active=true = active.
type Plugin struct{ ent.Schema }

func (Plugin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(), // the manifest id
		field.String("name").Default(""),
		field.String("version").Default(""),
		field.Time("installed_at").Optional().Nillable(),
		field.Bool("active").Default(false),
		field.String("path").Default(""),
		field.String("manifest_hash").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Plugin) Indexes() []ent.Index {
	return []ent.Index{index.Fields("active")}
}
```

> The manifest `id` is the primary key (string). `Optional().Nillable()` makes `installed_at` a `*time.Time` so "discovered" (nil) is distinguishable from "installed".

- [ ] **Step 2: Regenerate ent**

Run: `cd server && go generate ./internal/db/ent/...`. Verify `server/internal/db/ent/plugin/` exists and `ent.Client` gains `Plugin`. If `runtime.go`/`go.sum` show unrelated drift, `git checkout --` them (keep only the new `plugin`-entity generated files).

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...` → compiles.

- [ ] **Step 4: Commit**

```bash
git add server/internal/db/ent server/internal/db/ent/schema/plugin.go
git commit --no-gpg-sign -m "feat: add plugin ent schema (lifecycle state)"
```

---

## Task 3: `plugin_setting` ent schema

**Files:**
- Create: `server/internal/db/ent/schema/plugin_setting.go`

- [ ] **Step 1: Write the schema**

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PluginSetting stores one configurable value for a plugin. Secret values are
// AES-GCM ciphertext (base64) with nonce set; non-secret values are plaintext.
type PluginSetting struct{ ent.Schema }

func (PluginSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("id").Immutable(),
		field.String("plugin_id"),
		field.String("key"),
		field.String("value").Default(""),
		field.Bool("secret").Default(false),
		field.String("nonce").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PluginSetting) Indexes() []ent.Index {
	return []ent.Index{index.Fields("plugin_id", "key").Unique()}
}
```

- [ ] **Step 2: Regenerate ent** — `cd server && go generate ./internal/db/ent/...`; revert unrelated drift.

- [ ] **Step 3: Verify** — `cd server && go build ./...`.

- [ ] **Step 4: Commit**

```bash
git add server/internal/db/ent server/internal/db/ent/schema/plugin_setting.go
git commit --no-gpg-sign -m "feat: add plugin_setting ent schema (per-plugin config + secrets)"
```

---

## Task 4: `plugin` repo

**Files:**
- Create: `server/internal/db/repo/plugin_repo.go`
- Test: `server/internal/db/repo/plugin_repo_test.go`

- [ ] **Step 1: Write the failing test** (package `repo_test`, reuse `openTestDB(t)`)

```go
package repo_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestPluginRepo_Lifecycle(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewPluginRepo(client)
	ctx := t.Context()

	// Upsert (discover) a plugin
	_, err := r.Upsert(ctx, repo.UpsertPluginInput{ID: "p1", Name: "P1", Version: "1.0.0", Path: "/x", ManifestHash: "h1"})
	require.NoError(t, err)

	got, err := r.Get(ctx, "p1")
	require.NoError(t, err)
	assert.Nil(t, got.InstalledAt) // discovered
	assert.False(t, got.Active)

	now := time.Now()
	require.NoError(t, r.SetInstalledAt(ctx, "p1", &now))
	require.NoError(t, r.SetActive(ctx, "p1", true))
	got, _ = r.Get(ctx, "p1")
	require.NotNil(t, got.InstalledAt)
	assert.True(t, got.Active)

	all, err := r.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	require.NoError(t, r.Delete(ctx, "p1"))
	_, err = r.Get(ctx, "p1")
	assert.True(t, repo.IsNotFound(err))
}
```

- [ ] **Step 2: Run → fail** — `cd server && go test ./internal/db/repo/ -run TestPluginRepo -v` (undefined).

- [ ] **Step 3: Implement** `plugin_repo.go` (mirror `app_setting_repo.go` upsert style):

```go
package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/plugin"
)

type UpsertPluginInput struct {
	ID, Name, Version, Path, ManifestHash string
}

type PluginRepo interface {
	Get(ctx context.Context, id string) (*ent.Plugin, error)
	List(ctx context.Context) ([]*ent.Plugin, error)
	Upsert(ctx context.Context, in UpsertPluginInput) (*ent.Plugin, error)
	SetInstalledAt(ctx context.Context, id string, at *time.Time) error
	SetActive(ctx context.Context, id string, active bool) error
	SetVersion(ctx context.Context, id, version string) error
	Delete(ctx context.Context, id string) error
}

type entPluginRepo struct{ client *ent.Client }

func NewPluginRepo(client *ent.Client) PluginRepo { return &entPluginRepo{client: client} }

// IsNotFound exposes ent.IsNotFound for callers/tests.
func IsNotFound(err error) bool { return ent.IsNotFound(err) }

func (r *entPluginRepo) Get(ctx context.Context, id string) (*ent.Plugin, error) {
	p, err := r.client.Plugin.Get(ctx, id)
	if err != nil {
		return nil, err // includes ent.NotFoundError; callers use IsNotFound
	}
	return p, nil
}

func (r *entPluginRepo) List(ctx context.Context) ([]*ent.Plugin, error) {
	rows, err := r.client.Plugin.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin.List: %w", err)
	}
	return rows, nil
}

func (r *entPluginRepo) Upsert(ctx context.Context, in UpsertPluginInput) (*ent.Plugin, error) {
	err := r.client.Plugin.Create().
		SetID(in.ID).SetName(in.Name).SetVersion(in.Version).
		SetPath(in.Path).SetManifestHash(in.ManifestHash).
		OnConflictColumns(plugin.FieldID).
		// On re-discovery, refresh metadata but DO NOT reset installed_at/active.
		UpdateName().UpdateVersion().UpdatePath().UpdateManifestHash().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin.Upsert: %w", err)
	}
	return r.Get(ctx, in.ID)
}

func (r *entPluginRepo) SetInstalledAt(ctx context.Context, id string, at *time.Time) error {
	upd := r.client.Plugin.UpdateOneID(id)
	if at == nil {
		upd = upd.ClearInstalledAt()
	} else {
		upd = upd.SetInstalledAt(*at)
	}
	return upd.Exec(ctx)
}

func (r *entPluginRepo) SetActive(ctx context.Context, id string, active bool) error {
	return r.client.Plugin.UpdateOneID(id).SetActive(active).Exec(ctx)
}

func (r *entPluginRepo) SetVersion(ctx context.Context, id, version string) error {
	return r.client.Plugin.UpdateOneID(id).SetVersion(version).Exec(ctx)
}

func (r *entPluginRepo) Delete(ctx context.Context, id string) error {
	return r.client.Plugin.DeleteOneID(id).Exec(ctx)
}
```

> Verify generated names: `plugin.FieldID`, `UpdateName()` etc. on the OnConflict builder, `ClearInstalledAt()`/`SetInstalledAt()` on the update builder, `UpdateOneID`. Adjust to the generated API if different (check `server/internal/db/ent/plugin_update.go` + `plugin_create.go`).

- [ ] **Step 4: Run → pass** — `cd server && go test ./internal/db/repo/ -run TestPluginRepo -v`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/repo/plugin_repo.go server/internal/db/repo/plugin_repo_test.go
git commit --no-gpg-sign -m "feat: add plugin repo (lifecycle state CRUD)"
```

---

## Task 5: `plugin_setting` repo

**Files:**
- Create: `server/internal/db/repo/plugin_setting_repo.go`
- Test: `server/internal/db/repo/plugin_setting_repo_test.go`

- [ ] **Step 1: Failing test** (package `repo_test`)

```go
package repo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

func TestPluginSettingRepo_CRUD(t *testing.T) {
	client := openTestDB(t)
	r := repo.NewPluginSettingRepo(client)
	ctx := t.Context()

	_, err := r.Upsert(ctx, repo.PluginSettingInput{PluginID: "p1", Key: "endpoint", Value: "https://x", Secret: false})
	require.NoError(t, err)
	_, err = r.Upsert(ctx, repo.PluginSettingInput{PluginID: "p1", Key: "apiKey", Value: "ciph", Secret: true, Nonce: "n"})
	require.NoError(t, err)
	// upsert same key updates, no dup
	_, err = r.Upsert(ctx, repo.PluginSettingInput{PluginID: "p1", Key: "endpoint", Value: "https://y", Secret: false})
	require.NoError(t, err)

	rows, err := r.ListByPlugin(ctx, "p1")
	require.NoError(t, err)
	assert.Len(t, rows, 2)

	require.NoError(t, r.DeleteByPlugin(ctx, "p1"))
	rows, _ = r.ListByPlugin(ctx, "p1")
	assert.Empty(t, rows)
}
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** `plugin_setting_repo.go`:

```go
package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent/pluginsetting"
)

type PluginSettingInput struct {
	PluginID, Key, Value, Nonce string
	Secret                      bool
}

type PluginSettingRepo interface {
	ListByPlugin(ctx context.Context, pluginID string) ([]*ent.PluginSetting, error)
	Upsert(ctx context.Context, in PluginSettingInput) (*ent.PluginSetting, error)
	DeleteByPlugin(ctx context.Context, pluginID string) error
}

type entPluginSettingRepo struct{ client *ent.Client }

func NewPluginSettingRepo(client *ent.Client) PluginSettingRepo { return &entPluginSettingRepo{client: client} }

func (r *entPluginSettingRepo) ListByPlugin(ctx context.Context, pluginID string) ([]*ent.PluginSetting, error) {
	rows, err := r.client.PluginSetting.Query().
		Where(pluginsetting.PluginIDEQ(pluginID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("pluginsetting.ListByPlugin: %w", err)
	}
	return rows, nil
}

func (r *entPluginSettingRepo) Upsert(ctx context.Context, in PluginSettingInput) (*ent.PluginSetting, error) {
	err := r.client.PluginSetting.Create().
		SetID(uuid.New().String()).
		SetPluginID(in.PluginID).SetKey(in.Key).SetValue(in.Value).
		SetSecret(in.Secret).SetNonce(in.Nonce).
		OnConflictColumns(pluginsetting.FieldPluginID, pluginsetting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("pluginsetting.Upsert: %w", err)
	}
	row, err := r.client.PluginSetting.Query().
		Where(pluginsetting.PluginIDEQ(in.PluginID), pluginsetting.KeyEQ(in.Key)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("pluginsetting.Upsert reload: %w", err)
	}
	return row, nil
}

func (r *entPluginSettingRepo) DeleteByPlugin(ctx context.Context, pluginID string) error {
	_, err := r.client.PluginSetting.Delete().Where(pluginsetting.PluginIDEQ(pluginID)).Exec(ctx)
	return err
}
```

> `UpdateNewValues()` on a composite OnConflict updates `value/secret/nonce`. Verify generated predicate names (`pluginsetting.FieldPluginID`, `FieldKey`, `PluginIDEQ`, `KeyEQ`).

- [ ] **Step 4: Run → pass.**

- [ ] **Step 5: Commit**

```bash
git add server/internal/db/repo/plugin_setting_repo.go server/internal/db/repo/plugin_setting_repo_test.go
git commit --no-gpg-sign -m "feat: add plugin_setting repo"
```

---

## Task 6: `secretbox` — AES-GCM + master key

**Files:**
- Create: `server/internal/secretbox/secretbox.go`
- Test: `server/internal/secretbox/secretbox_test.go`

- [ ] **Step 1: Failing test**

```go
package secretbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBox_EncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32) // all-zero key is fine for the round-trip test
	box, err := New(key)
	require.NoError(t, err)

	ct, nonce, err := box.Encrypt("super-secret-api-key")
	require.NoError(t, err)
	assert.NotEmpty(t, ct)
	assert.NotEmpty(t, nonce)
	assert.NotContains(t, ct, "super-secret") // ciphertext is opaque

	pt, err := box.Decrypt(ct, nonce)
	require.NoError(t, err)
	assert.Equal(t, "super-secret-api-key", pt)
}

func TestBox_WrongKeyFails(t *testing.T) {
	a, _ := New(make([]byte, 32))
	bKey := make([]byte, 32)
	bKey[0] = 1
	b, _ := New(bKey)
	ct, nonce, _ := a.Encrypt("x")
	_, err := b.Decrypt(ct, nonce)
	require.Error(t, err)
}

func TestNew_RejectsBadKeyLen(t *testing.T) {
	_, err := New(make([]byte, 16))
	require.Error(t, err)
}
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** `secretbox.go`:

```go
// Package secretbox provides authenticated symmetric encryption (AES-256-GCM)
// for secret values stored at rest, plus master-key bootstrapping.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const secretKeyFile = ".claude/dashboard-secret.key"

// Box encrypts/decrypts strings with AES-256-GCM.
type Box struct{ aead cipher.AEAD }

// New builds a Box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secretbox: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt returns base64 ciphertext + base64 nonce.
func (b *Box) Encrypt(plaintext string) (string, string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	ct := b.aead.Seal(nil, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), base64.StdEncoding.EncodeToString(nonce), nil
}

// Decrypt reverses Encrypt.
func (b *Box) Decrypt(ciphertextB64, nonceB64 string) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return "", err
	}
	pt, err := b.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secretbox: decrypt failed: %w", err)
	}
	return string(pt), nil
}

// LoadOrGenerateMasterKey resolves the 32-byte master key: from `existing`
// (DASHBOARD_SECRET_KEY hex) if set; else the persisted file; else generated +
// persisted to ~/.claude/dashboard-secret.key (0600). Mirrors the hooks-secret
// bootstrap.
func LoadOrGenerateMasterKey(existing string) ([]byte, error) {
	if existing != "" {
		key, err := hex.DecodeString(strings.TrimSpace(existing))
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("secretbox: DASHBOARD_SECRET_KEY must be 64 hex chars (32 bytes)")
		}
		return key, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, secretKeyFile)
	data, err := os.ReadFile(path)
	if err == nil {
		key, derr := hex.DecodeString(strings.TrimSpace(string(data)))
		if derr == nil && len(key) == 32 {
			return key, nil
		}
		slog.Warn("dashboard-secret.key invalid, regenerating", "path", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secretbox: read key file %s: %w", path, err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, err
	}
	slog.Info("Generated plugin secret master key — set DASHBOARD_SECRET_KEY to use across machines", "path", path)
	return key, nil
}
```

- [ ] **Step 4: Run → pass** — `cd server && go test ./internal/secretbox/ -v`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/secretbox/
git commit --no-gpg-sign -m "feat: add secretbox (AES-GCM at-rest encryption + master key)"
```

---

## Task 7: `pluginsettings` service

**Files:**
- Create: `server/internal/pluginsettings/service.go`
- Test: `server/internal/pluginsettings/service_test.go`

The service merges a plugin's manifest setting schema with stored values: returns the schema + values (secrets masked) for the UI, persists values (encrypting secret fields), and decrypts secret values for env injection (SP2).

- [ ] **Step 1: Failing test**

```go
package pluginsettings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

type fakeRepo struct{ rows map[string]row }
type row struct{ value, nonce string; secret bool }

func (f *fakeRepo) ListByPlugin(_ context.Context, _ string) ([]Stored, error) {
	out := []Stored{}
	for k, r := range f.rows {
		out = append(out, Stored{Key: k, Value: r.value, Nonce: r.nonce, Secret: r.secret})
	}
	return out, nil
}
func (f *fakeRepo) Upsert(_ context.Context, _ string, s Stored) error {
	f.rows[s.Key] = row{s.Value, s.Nonce, s.Secret}; return nil
}
func (f *fakeRepo) DeleteByPlugin(_ context.Context, _ string) error { f.rows = map[string]row{}; return nil }

func TestService_PutEncryptsSecret_GetMasks(t *testing.T) {
	box, _ := secretbox.New(make([]byte, 32))
	repo := &fakeRepo{rows: map[string]row{}}
	svc := New(repo, box)
	schema := []plugin.SettingField{
		{Key: "endpoint", Type: "url"},
		{Key: "apiKey", Type: "string", Secret: true},
	}
	ctx := context.Background()

	require.NoError(t, svc.Put(ctx, "p1", schema, map[string]string{"endpoint": "https://x", "apiKey": "KEY123"}))

	// stored secret is encrypted (not plaintext)
	assert.NotEqual(t, "KEY123", repo.rows["apiKey"].value)
	assert.True(t, repo.rows["apiKey"].secret)

	// Get masks secrets, shows non-secret values
	view, err := svc.Get(ctx, "p1", schema)
	require.NoError(t, err)
	assert.Equal(t, "https://x", view["endpoint"])
	assert.Equal(t, MaskedSentinel, view["apiKey"])

	// PUT with the sentinel leaves the secret unchanged
	require.NoError(t, svc.Put(ctx, "p1", schema, map[string]string{"apiKey": MaskedSentinel}))
	dec, err := svc.Decrypted(ctx, "p1", schema)
	require.NoError(t, err)
	assert.Equal(t, "KEY123", dec["apiKey"])
}
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** `service.go`:

```go
// Package pluginsettings manages per-plugin configuration values, encrypting
// secret fields at rest and masking them in API responses.
package pluginsettings

import (
	"context"
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
	"github.com/lx-wnk/agent-dashboard/server/internal/secretbox"
)

// MaskedSentinel is returned for secret values and, when sent back on Put,
// signals "leave unchanged".
const MaskedSentinel = "********"

// Stored is one persisted setting row (storage-agnostic).
type Stored struct {
	Key, Value, Nonce string
	Secret            bool
}

// Repo is the persistence the service needs.
type Repo interface {
	ListByPlugin(ctx context.Context, pluginID string) ([]Stored, error)
	Upsert(ctx context.Context, pluginID string, s Stored) error
	DeleteByPlugin(ctx context.Context, pluginID string) error
}

type Service struct {
	repo Repo
	box  *secretbox.Box
}

func New(repo Repo, box *secretbox.Box) *Service { return &Service{repo: repo, box: box} }

func (s *Service) load(ctx context.Context, pluginID string) (map[string]Stored, error) {
	rows, err := s.repo.ListByPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]Stored, len(rows))
	for _, r := range rows {
		m[r.Key] = r
	}
	return m, nil
}

func isSecret(schema []plugin.SettingField, key string) bool {
	for _, f := range schema {
		if f.Key == key {
			return f.Secret
		}
	}
	return false
}

// Get returns key->value for the schema; secret values are masked.
func (s *Service) Get(ctx context.Context, pluginID string, schema []plugin.SettingField) (map[string]string, error) {
	stored, err := s.load(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range schema {
		r, ok := stored[f.Key]
		if !ok {
			out[f.Key] = ""
			continue
		}
		if f.Secret {
			out[f.Key] = MaskedSentinel
		} else {
			out[f.Key] = r.Value
		}
	}
	return out, nil
}

// Put persists values. Secret fields are encrypted; a secret submitted as the
// masked sentinel is skipped (left unchanged). Unknown keys (not in schema) are
// rejected.
func (s *Service) Put(ctx context.Context, pluginID string, schema []plugin.SettingField, values map[string]string) error {
	known := map[string]bool{}
	for _, f := range schema {
		known[f.Key] = true
	}
	for k := range values {
		if !known[k] {
			return fmt.Errorf("pluginsettings: unknown key %q", k)
		}
	}
	for _, f := range schema {
		v, ok := values[f.Key]
		if !ok {
			continue
		}
		if f.Secret {
			if v == MaskedSentinel {
				continue // unchanged
			}
			ct, nonce, err := s.box.Encrypt(v)
			if err != nil {
				return err
			}
			if err := s.repo.Upsert(ctx, pluginID, Stored{Key: f.Key, Value: ct, Nonce: nonce, Secret: true}); err != nil {
				return err
			}
			continue
		}
		if err := s.repo.Upsert(ctx, pluginID, Stored{Key: f.Key, Value: v, Secret: false}); err != nil {
			return err
		}
	}
	return nil
}

// Decrypted returns key->plaintext (secrets decrypted) for env injection (SP2).
func (s *Service) Decrypted(ctx context.Context, pluginID string, schema []plugin.SettingField) (map[string]string, error) {
	stored, err := s.load(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range schema {
		r, ok := stored[f.Key]
		if !ok {
			continue
		}
		if r.Secret {
			pt, derr := s.box.Decrypt(r.Value, r.Nonce)
			if derr != nil {
				return nil, derr
			}
			out[f.Key] = pt
		} else {
			out[f.Key] = r.Value
		}
	}
	return out, nil
}

// Clear removes all settings for a plugin (called on uninstall).
func (s *Service) Clear(ctx context.Context, pluginID string) error {
	return s.repo.DeleteByPlugin(ctx, pluginID)
}
```

- [ ] **Step 4: Run → pass.** Then add an adapter so the ent `PluginSettingRepo` satisfies `pluginsettings.Repo` (map `Stored`↔`PluginSettingInput`/`ent.PluginSetting`) — put it in di.go (Task 11) or a tiny adapter file. The unit test uses the fake, so this can wait for wiring.

- [ ] **Step 5: Commit**

```bash
git add server/internal/pluginsettings/
git commit --no-gpg-sign -m "feat: add pluginsettings service (encrypt/mask/decrypt)"
```

---

## Task 8: `pluginlifecycle` engine

**Files:**
- Create: `server/internal/pluginlifecycle/engine.go`
- Test: `server/internal/pluginlifecycle/engine_test.go`

State machine + hook caller. The hook caller is an interface so tests fake it; the real HTTP caller is trivial and wired in di.go (process orchestration to make a plugin reachable is SP2).

- [ ] **Step 1: Failing test**

```go
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
func (f *fakePluginRepo) SetInstalledAt(_ context.Context, _ string, at *time.Time) error { f.installedAt = at; return nil }
func (f *fakePluginRepo) SetActive(_ context.Context, _ string, a bool) error { f.active = a; return nil }
func (f *fakePluginRepo) SetVersion(_ context.Context, _ string, v string) error { f.version = v; return nil }

type recordingHooks struct{ called []string; failOn string }

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
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** `engine.go`:

```go
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
	if err := e.settings.Clear(ctx, d.ID); err != nil {
		return err
	}
	return e.repo.SetInstalledAt(ctx, d.ID, nil)
}
```

> The `PluginRepo` (Task 4) doesn't have `GetState`; add a thin adapter in di.go mapping `ent.Plugin` → `pluginlifecycle.State`, or add `GetState` to PluginRepo. Adapter preferred (keeps the engine's interface narrow).

- [ ] **Step 4: Run → pass** — `cd server && go test ./internal/pluginlifecycle/ -v`.

- [ ] **Step 5: Implement the real HTTP HookCaller** in the same package:

```go
// engine_http.go
package pluginlifecycle

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// HTTPHookCaller POSTs hooks to the plugin's Addr. Reachability (process running)
// is SP2's concern; here a non-2xx or transport error fails the transition.
type HTTPHookCaller struct{ client *http.Client }

func NewHTTPHookCaller() *HTTPHookCaller {
	return &HTTPHookCaller{client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *HTTPHookCaller) Call(ctx context.Context, d plugin.Descriptor, hook string) error {
	url := "http://" + d.Addr + hook
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("hook %s unreachable: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hook %s returned %d", url, resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 6: Commit**

```bash
git add server/internal/pluginlifecycle/
git commit --no-gpg-sign -m "feat: add plugin lifecycle engine (state machine + HTTP hooks)"
```

---

## Task 9: Discovery

**Files:**
- Create: `server/internal/pluginlifecycle/discovery.go`
- Test: `server/internal/pluginlifecycle/discovery_test.go`

- [ ] **Step 1: Failing test** — write `plugin.json` files into a temp dir, run `Discover`, assert rows upserted + a manifest_hash change flags update-available.

```go
package pluginlifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memDiscoverRepo struct{ rows map[string]DiscoverRow }
type DiscoverRow struct{ Version, ManifestHash string }

func (m *memDiscoverRepo) UpsertDiscovered(_ context.Context, in DiscoveredPlugin) (bool, error) {
	prev, existed := m.rows[in.ID]
	m.rows[in.ID] = DiscoverRow{in.Version, in.ManifestHash}
	updateAvailable := existed && prev.ManifestHash != in.ManifestHash
	return updateAvailable, nil
}

func writeManifest(t *testing.T, dir, id, version string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, id), 0o755))
	body := `{"id":"` + id + `","version":"` + version + `","capabilities":["route_extension"],"addr":"127.0.0.1:1"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, id, "plugin.json"), []byte(body), 0o644))
}

func TestDiscover_UpsertsAndDetectsChange(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "p1", "1.0.0")
	repo := &memDiscoverRepo{rows: map[string]DiscoverRow{}}
	d := NewDiscoverer(dir, repo)

	res, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Found)
	assert.Empty(t, res.UpdatesAvailable)

	// version bump → manifest hash change → update available
	writeManifest(t, dir, "p1", "2.0.0")
	res, err = d.Discover(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.UpdatesAvailable, "p1")
}
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** `discovery.go`:

```go
package pluginlifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

// DiscoveredPlugin is the upsert payload for one found manifest.
type DiscoveredPlugin struct {
	ID, Name, Version, Path, ManifestHash string
}

// DiscoverRepo upserts a discovered plugin and reports whether its manifest
// changed since the stored row (update-available).
type DiscoverRepo interface {
	UpsertDiscovered(ctx context.Context, in DiscoveredPlugin) (updateAvailable bool, err error)
}

type Discoverer struct {
	dir  string
	repo DiscoverRepo
}

func NewDiscoverer(dir string, repo DiscoverRepo) *Discoverer { return &Discoverer{dir: dir, repo: repo} }

// Result summarizes a discovery pass.
type Result struct {
	Found            int
	UpdatesAvailable []string
}

func (d *Discoverer) Discover(ctx context.Context) (Result, error) {
	var res Result
	if d.dir == "" {
		return res, nil
	}
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, fmt.Errorf("discover: read dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(d.dir, e.Name())
		raw, err := os.ReadFile(filepath.Join(path, "plugin.json"))
		if err != nil {
			continue // no manifest — skip (existing behavior)
		}
		var desc plugin.Descriptor
		if err := json.Unmarshal(raw, &desc); err != nil || desc.ID == "" {
			continue
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		upd, err := d.repo.UpsertDiscovered(ctx, DiscoveredPlugin{
			ID: desc.ID, Name: desc.Name, Version: desc.Version, Path: path, ManifestHash: hash,
		})
		if err != nil {
			return res, err
		}
		res.Found++
		if upd {
			res.UpdatesAvailable = append(res.UpdatesAvailable, desc.ID)
		}
	}
	return res, nil
}
```

> The ent-backed `DiscoverRepo` adapter (in di.go, Task 11) maps `UpsertDiscovered` → `PluginRepo.Get` (read old hash) + `PluginRepo.Upsert` (which preserves installed_at/active), returning `oldHash != newHash`.

- [ ] **Step 4: Run → pass.**

- [ ] **Step 5: Commit**

```bash
git add server/internal/pluginlifecycle/discovery.go server/internal/pluginlifecycle/discovery_test.go
git commit --no-gpg-sign -m "feat: add plugin discovery (scan + manifest-hash change detection)"
```

---

## Task 10: Lifecycle + settings API

**Files:**
- Modify: `server/internal/api/plugins/handler.go`
- Test: `server/internal/api/plugins/handler_test.go`
- Modify: `server/internal/api/router.go` (RouterDeps)

The handler depends on small interfaces (lifecycle engine + a plugin lister + the pluginsettings service + a manifest provider) so it can be faked in tests. Mirror the existing handler's narrow-DTO + `apierr.ErrorMiddleware` patterns.

- [ ] **Step 1: Failing test** — covering: `GET /api/plugins` returns `{id,name,version,state,updateAvailable,capabilities,hasSettings}` (no secrets/BaseURL); `POST /api/plugins/{id}/activate` calls the engine + returns new state; unknown id → 400; hook failure → 500; `GET /api/plugins/{id}/settings` masks secrets; `PUT` validates + persists. Write concrete assertions with a fake controller exposing:

```go
type controller interface {
	List(ctx context.Context) ([]PluginView, error)
	Transition(ctx context.Context, id, action string) (PluginView, error) // action: install|activate|deactivate|uninstall
	GetSettings(ctx context.Context, id string) (schema []plugin.SettingField, values map[string]string, err error)
	PutSettings(ctx context.Context, id string, values map[string]string) error
}
```
Sentinels: `ErrUnknownPlugin` (→400), `ErrInvalidAction` (→400); other errors →500.

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** the handler:
- `PluginView{ID,Name,Version,State,UpdateAvailable,Capabilities,HasSettings}` (JSON camelCase). NEVER include `BaseURL`/`Env`.
- `Mount(r)`: `GET /api/plugins`, `POST /api/plugins/{id}/{action}` (validate action ∈ {install,activate,deactivate,uninstall}), `GET/PUT /api/plugins/{id}/settings`.
- Error mapping: `errors.Is(err, ErrUnknownPlugin)` or `ErrInvalidAction` → `apierr.ErrBadRequest` (400); else wrap plain (500). Settings GET returns `{schema, values}`; secret values already masked by the service.

Full handler code follows the providers/settings handler shape (Task 6 of the #230 plan). Write it out completely — `chi.URLParam`, `json.NewDecoder`, `apierr.ErrorMiddleware` wrappers in `Mount`.

- [ ] **Step 4: Implement the concrete controller** (`server/internal/pluginsctl/lifecycle_controller.go` or extend pluginsctl): wires discovery+lifecycle engine+plugin repo+pluginsettings service+manifest lookup (read `plugin.json` from the plugin's `path` for the descriptor/schema). `List` reads `PluginRepo.List` + derives state + `hasSettings = len(manifest.Settings)>0`. `Transition` loads the descriptor from disk, dispatches to the engine method. `Get/PutSettings` load the manifest schema, delegate to pluginsettings.Service.

- [ ] **Step 5: Wire RouterDeps + di.go route** — add `PluginsHandler` (or reuse) to `RouterDeps`, nil-guarded mount in `router.go`; construct in di.go (Task 11).

- [ ] **Step 6: Run → pass** — `cd server && go test ./internal/api/plugins/ -v && go build ./...`.

- [ ] **Step 7: Commit**

```bash
git add server/internal/api/plugins server/internal/pluginsctl server/internal/api/router.go
git commit --no-gpg-sign -m "feat: plugin lifecycle + settings API"
```

---

## Task 11: di.go wiring + adapters

**Files:**
- Modify: `server/cmd/serve/di.go`

- [ ] **Step 1:** Construct, when `entClient != nil`: master key (`secretbox.LoadOrGenerateMasterKey(os.Getenv("DASHBOARD_SECRET_KEY"))` → `secretbox.New`), `pluginRepo`, `pluginSettingRepo`, the `pluginsettings.Service` (with a repo adapter mapping `Stored`↔ent), the `pluginlifecycle.Engine` (with a `StateRepo` adapter over `pluginRepo`, `NewHTTPHookCaller()`, and the pluginsettings service as the `SettingsClearer`), the `Discoverer` (with a `DiscoverRepo` adapter), the lifecycle controller + handler. Run `Discoverer.Discover(ctx)` once at boot.
- [ ] **Step 2:** Write the small adapters (Stored↔ent.PluginSetting; ent.Plugin→pluginlifecycle.State; DiscoverRepo over PluginRepo.Get+Upsert). Place them in di.go or a `server/cmd/serve/plugin_adapters.go`.
- [ ] **Step 3:** Build: `cd server && go build ./...`. Verify with `go test ./internal/api/plugins/ ./internal/pluginlifecycle/ ./internal/pluginsettings/ ./cmd/serve/`.
- [ ] **Step 4: Commit**

```bash
git add server/cmd/serve/
git commit --no-gpg-sign -m "feat: wire plugin lifecycle/settings/discovery at startup"
```

---

## Task 12: Migration from #230 `plugins.enabled` + boot predicate

**Files:**
- Modify: `server/cmd/serve/di.go` (boot predicate)
- Modify: `server/internal/settings/registry.go` (remove `plugins.enabled`)
- Create: a one-time seed in di.go (or `plugin_migrate.go`)
- Test: `server/cmd/serve/plugin_migrate_test.go`

- [ ] **Step 1: Failing test** — given an `app_setting` `plugins.enabled = "p1,p2"` and empty `plugin` table, after `seedPluginsFromEnabledList(ctx, settingsSvc, pluginRepo)` the `plugin` table has p1,p2 with `active=true, installed_at!=nil`; idempotent on re-run (no dupes).

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** the seed: read `settingsSvc.StringSlice("plugins.enabled")`; for each id, if no `plugin` row, `Upsert` + `SetInstalledAt(now)` + `SetActive(true)`. Run once at boot (before discovery, or guarded so discovery doesn't clobber active). Change the boot enablement predicate (the `pluginRegistry.SetEnabled(...)` closure) to read `active` from `pluginRepo.List` instead of `settingsSvc.StringSlice("plugins.enabled")`. Remove the `plugins.enabled` Definition from `settings/registry.go` (and its managed flag).

- [ ] **Step 4: Run → pass + build** — `cd server && go build ./... && go test ./cmd/serve/ ./internal/settings/`.

- [ ] **Step 5: Commit**

```bash
git add server/cmd/serve server/internal/settings/registry.go
git commit --no-gpg-sign -m "feat: migrate plugins.enabled to plugin table; boot predicate reads table"
```

---

## Task 13: Docs

**Files:**
- Modify: `docs/guides/configuration.md` (or a new `docs/guides/plugins.md`), `CHANGELOG.md`, `.env.dist`

- [ ] **Step 1:** Document `DASHBOARD_SECRET_KEY` (env, 64 hex chars; auto-generated to `~/.claude/dashboard-secret.key` if unset) in the bootstrap/secrets section; note it encrypts plugin secret settings and losing it means re-entering plugin secrets.
- [ ] **Step 2:** Add a "Plugin lifecycle" doc section: manifest v2 fields (slots/settings/lifecycle/permissions), states (discovered/inactive/active), the lifecycle API endpoints, that settings secrets are encrypted at rest, and that activation effects (route/ui serving) land with SP2/SP4.
- [ ] **Step 3:** `CHANGELOG.md` `[Unreleased]`: add the plugin lifecycle foundation (DB-backed plugin state, per-plugin settings with encrypted secrets, lifecycle API, manifest v2); note `plugins.enabled` setting is superseded by the `plugin` table (migration automatic).
- [ ] **Step 4:** `.env.dist`: add `DASHBOARD_SECRET_KEY` (commented, with the hex note).
- [ ] **Step 5:** `pnpm lint` (markdown). Commit:

```bash
git add docs/ CHANGELOG.md .env.dist
git commit --no-gpg-sign -m "docs: document plugin lifecycle foundation + DASHBOARD_SECRET_KEY"
```

---

## Final verification (before PR)

- [ ] `cd server && go build ./...` clean.
- [ ] Targeted tests green: `go test ./internal/plugin/ ./internal/db/repo/ ./internal/secretbox/ ./internal/pluginsettings/ ./internal/pluginlifecycle/ ./internal/api/plugins/ ./cmd/serve/ ./internal/settings/`.
- [ ] `golangci-lint run ./internal/secretbox/... ./internal/pluginsettings/... ./internal/pluginlifecycle/... ./internal/api/plugins/...` → 0 issues.
- [ ] Restore ent tree if any full-test run drifted it: `git checkout -- server/internal/db/ent/`.
- [ ] Manual: a v1 plugin.json still loads; a v2 manifest's settings appear in `GET /api/plugins/{id}/settings` with secrets masked; setting a secret then reading shows the sentinel; the `plugin` table seeds from a prior `plugins.enabled`.

## Self-review notes (author)

- Spec coverage: manifest v2 (T1), plugin table (T2/T4), plugin_setting + secrets (T3/T5/T6/T7), lifecycle engine + hooks (T8), discovery (T9), API (T10), wiring (T11), #230 migration (T12), docs (T13). SP1/SP2 boundary honored — engine calls hooks via an interface; process orchestration deferred.
- Type consistency: `Descriptor` v2 fields, `secretbox.Box`/`New`/`Encrypt`/`Decrypt`/`LoadOrGenerateMasterKey`, `pluginsettings` `Stored`/`MaskedSentinel`/`Get`/`Put`/`Decrypted`/`Clear`, `pluginlifecycle` `State`/`StateRepo`/`HookCaller`/`Engine` + `Install/Activate/Deactivate/Update/Uninstall`, `Discoverer`/`DiscoverRepo`/`DiscoveredPlugin`/`Result` used consistently across tasks.
- Deferred to later SPs: process start/stop + catch-all dispatch (SP2), restart (SP3), frontend slots + per-plugin settings UI + override/extend chain (SP4), SDK docs (SP5).
