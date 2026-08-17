# Plugin SP9 — Manifest Field Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove three dead manifest surface areas — `Descriptor.Slots`/`SlotBinding`, `Descriptor.Permissions`, and `Registry.AllWithCapability` — identified in the post-SP5 architecture audit.

**Architecture:** Pure subtraction across three layers: Go types (`server/internal/plugin/types.go` and `registry.go`), JSON Schema (`plugin-sdk/plugin.schema.json`), and three doc files. No database changes, no new types, no new endpoints. Forward-compat is preserved by `json.Unmarshal`'s unknown-field tolerance and `additionalProperties: true` in the schema, so old manifests with `slots`/`permissions` still load without error.

**Tech Stack:** Go 1.26 backend, Vue 3 TS frontend, ent ORM, vitest, go test

---

## Gotchas (read before starting)

- `go test ./...` from `server/` **regenerates the entire `server/internal/db/ent/` tree** and can corrupt it (unused imports → build failure). Always scope go test to the touched package: `go test ./internal/plugin/`. If you do run `./...`, restore immediately with `git checkout -- server/internal/db/ent/`.
- All commits must use `--no-gpg-sign` (SSH signing hangs in this env).
- No ent changes in this spec — never touch `server/internal/db/ent/`.

---

## Task 1: Update manifest_test.go — strip stale Slots assertions, add forward-compat test

**Files**
- Modify: `server/internal/plugin/manifest_test.go` (full file, currently 43 lines)

The existing `TestDescriptor_ParsesV2` asserts `d.Slots[0].Slot` etc. (lines 24–27) and `TestDescriptor_BackwardCompatV1` asserts `d.Slots` is empty (line 39). Both break compilation once the field is removed. Update them first so the type removal in Task 2 is a clean no-error drop.

- [ ] **Step 1: Run baseline — confirm current state passes**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/plugin/ -run TestDescriptor -v
```

Expected (3 tests, all green — Slots field still exists at this point):
```
=== RUN   TestDescriptor_ParsesV2
--- PASS: TestDescriptor_ParsesV2
=== RUN   TestDescriptor_BackwardCompatV1
--- PASS: TestDescriptor_BackwardCompatV1
```

- [ ] **Step 2: Replace the full test file content**

Write `server/internal/plugin/manifest_test.go` as follows. The raw JSON strings intentionally retain `slots` and `permissions` — leaving unknown fields in the JSON proves `json.Unmarshal` silently drops them (forward-compat). The `d.Slots` struct-field assertions are gone because the field will be removed.

```go
package plugin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptor_ParsesV2(t *testing.T) {
	// slots/permissions remain in the raw JSON intentionally: they are removed from
	// Descriptor but still present in old manifests — json.Unmarshal must not error.
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
	assert.Empty(t, d.Settings)
	assert.Empty(t, d.Name)
}

func TestDescriptor_LegacySlotsAndPermissionsIgnored(t *testing.T) {
	// A manifest carrying the removed slots/permissions fields must parse without
	// error — json.Unmarshal ignores unknown JSON fields by default.
	raw := `{
	  "id":"legacy-plugin","capabilities":["route_extension"],"addr":"127.0.0.1:19020",
	  "slots":[{"slot":"agent-toolbar","priority":5,"mode":"override"}],
	  "permissions":["network:outbound","fs:read"]
	}`
	var d Descriptor
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	assert.Equal(t, "legacy-plugin", d.ID)
	assert.Equal(t, []string{"route_extension"}, d.Capabilities)
}
```

- [ ] **Step 3: Run tests — must still pass (Slots field still exists in types.go)**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/plugin/ -run TestDescriptor -v
```

Expected: 3 tests pass (including the new `TestDescriptor_LegacySlotsAndPermissionsIgnored`).

- [ ] **Step 4: Commit**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git add server/internal/plugin/manifest_test.go
git commit --no-gpg-sign -m "test(plugin): drop Slots assertions, add forward-compat test for legacy manifest fields"
```

---

## Task 2: Remove SlotBinding, Slots, Permissions from types.go

**Files**
- Modify: `server/internal/plugin/types.go`
  - Delete line 17: `Slots       []SlotBinding  \`json:"slots"\``
  - Delete line 20: `Permissions []string       \`json:"permissions"\``
  - Delete lines 23–30: the `SlotBinding` struct and its comment block

- [ ] **Step 1: Verify zero non-definition references to removed symbols**

```
grep -rn "SlotBinding\|\.Slots\b\|\.Permissions\b" \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/server/
```

Expected: only the definition lines in `types.go` and `manifest_test.go` (the latter's `d.Slots` assertions were already removed in Task 1). If any caller appears, fix it before proceeding.

- [ ] **Step 2: Apply the edit — replace types.go**

The file after editing (full content — verify the ent-adjacent package comment is preserved):

```go
// Package plugin provides runtime plugin discovery and lifecycle management.
package plugin

// Descriptor is read from plugin.json in each plugin directory.
type Descriptor struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	// Addr is the HTTP address the plugin listens on (e.g. "127.0.0.1:13200").
	Addr string `json:"addr"`
	// Command is the executable + args to start the plugin process.
	// If empty, the plugin is expected to already be running.
	Command   []string       `json:"command"`
	Env       []string       `json:"env"`
	Settings  []SettingField `json:"settings"`
	Lifecycle LifecycleHooks `json:"lifecycle"`
}

// SettingField declares one configurable setting. Secret fields are encrypted at
// rest and masked in the API.
type SettingField struct {
	Key    string   `json:"key"`
	Type   string   `json:"type"` // string|url|int|bool|enum
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

// HasCapability reports whether the plugin declares the given capability.
func (d Descriptor) HasCapability(capability string) bool {
	for _, c := range d.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// Capability constants used in plugin.json.
const (
	CapAuthProvider   = "auth_provider"
	CapRouteExtension = "route_extension"
	// CapUIExtension marks a plugin that contributes frontend UI into named slots
	// via a ui-manifest.json + per-slot JS modules served by the plugin proxy.
	CapUIExtension = "ui_extension"
)
```

- [ ] **Step 3: Build — no stale references allowed**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go build ./...
```

Expected: zero output (clean build). Any compile error means a stale caller survived — fix it before continuing.

- [ ] **Step 4: Run plugin package tests**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/plugin/
```

Expected:
```
ok      github.com/lx-wnk/agent-dashboard/server/internal/plugin   <duration>s
```

- [ ] **Step 5: Grep-verify complete removal**

```
grep -rn "SlotBinding\|\.Slots\b" \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/server/
```

Expected: no output.

- [ ] **Step 6: Commit**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git add server/internal/plugin/types.go
git commit --no-gpg-sign -m "refactor(plugin): remove Slots/SlotBinding/Permissions from Descriptor"
```

---

## Task 3: Remove AllWithCapability from registry.go

**Files**
- Modify: `server/internal/plugin/registry.go` (lines 402–413 + blank line before)

- [ ] **Step 1: Confirm zero callers**

```
grep -rn "AllWithCapability" \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/server/
```

Expected output — only the definition lines, no callers:
```
server/internal/plugin/registry.go:402:// AllWithCapability returns all plugins with the given capability.
server/internal/plugin/registry.go:403:func (r *Registry) AllWithCapability(capability string) []Entry {
```

If a caller appears, do not delete the method — report a blocker.

- [ ] **Step 2: Delete the method block from registry.go**

Remove lines 401–413 (blank line + comment + function body):

```go
// AllWithCapability returns all plugins with the given capability.
func (r *Registry) AllWithCapability(capability string) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Entry
	for _, p := range r.plugins {
		if p.Descriptor.HasCapability(capability) {
			out = append(out, p)
		}
	}
	return out
}
```

The method immediately before (`FindByCapability`, ending around line 400) and after (`HasAttemptedCapability`, starting around line 415) must remain intact.

- [ ] **Step 3: Build and test**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go build ./... && go test ./internal/plugin/
```

Expected:
```
ok      github.com/lx-wnk/agent-dashboard/server/internal/plugin   <duration>s
```

- [ ] **Step 4: Grep-verify**

```
grep -rn "AllWithCapability" \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/server/
```

Expected: no output.

- [ ] **Step 5: Commit**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git add server/internal/plugin/registry.go
git commit --no-gpg-sign -m "refactor(plugin): remove unused AllWithCapability registry method"
```

---

## Task 4: Trim plugin.schema.json

**Files**
- Modify: `plugin-sdk/plugin.schema.json`
  - Delete lines 20–31: the `"slots"` property block
  - Delete line 57: the `"permissions"` property line

- [ ] **Step 1: Apply the edit**

The `slots` block to remove (lines 20–31):

```json
"slots": {
  "type": "array",
  "items": {
    "type": "object",
    "required": ["slot"],
    "properties": {
      "slot": { "type": "string" },
      "priority": { "type": "integer" },
      "mode": { "enum": ["override", "extend"] }
    }
  }
},
```

The `permissions` line to remove (line 57):

```json
"permissions": { "type": "array", "items": { "type": "string" } }
```

After the edit, confirm `"additionalProperties": true` is still present (line 7 of the original — it must remain so old manifests with `slots`/`permissions` still satisfy the schema).

The final schema `"properties"` block contains only: `$schema`, `id`, `name`, `version`, `capabilities`, `addr`, `command`, `env`, `settings`, `lifecycle`.

- [ ] **Step 2: Run the drift-guard test**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test -- --reporter=verbose src/plugin-sdk.test.ts
```

Expected: all 4 tests pass. None of the 5 example plugins carry `slots` or `permissions`, so removing those schema properties has no effect on validation of the corpus.

- [ ] **Step 3: Commit**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git add plugin-sdk/plugin.schema.json
git commit --no-gpg-sign -m "refactor(plugin): remove slots and permissions from plugin.json schema"
```

---

## Task 5: Update docs

**Files**
- Modify: `docs/plugin-guide.md`
- Modify: `plugin-sdk/README.md`
- Modify: `CHANGELOG.md`

### 5a — docs/plugin-guide.md

- [ ] **Step 1: Update example JSON block (lines 35–58)**

Remove `"slots"` and `"permissions"` from the example. Replace the full block with:

```json
{
  "$schema": "../../plugin-sdk/plugin.schema.json",
  "id":           "my-plugin",
  "name":         "My Plugin",
  "version":      "1.0.0",
  "capabilities": ["route_extension"],
  "addr":         "127.0.0.1:19010",
  "command":      ["./my-plugin"],
  "env":          ["MY_API_KEY"],
  "settings": [
    { "key": "api_key", "type": "string", "label": "API Key", "secret": true },
    { "key": "mode",    "type": "enum",   "label": "Mode", "enum": ["fast", "slow"] }
  ],
  "lifecycle": {
    "activate":   "/hooks/activate",
    "deactivate": "/hooks/deactivate"
  }
}
```

- [ ] **Step 2: Remove `### slots — UI slot bindings` section (lines 75–84)**

Delete the entire block:

```
### `slots` — UI slot bindings

Declares which dashboard UI slots the plugin contributes to. Consumed by the frontend when
`ui_extension` is active.

| Field      | Description |
|------------|-------------|
| `slot`     | Target slot name (see the slot table under `ui_extension`). |
| `priority` | Render order — higher renders outer/first. Default `0`. |
| `mode`     | `"override"` (exclusive — replaces all others), `"extend"` (wraps the parent chain, receives `parent` handle), or omit for sibling (default — rendered alongside others). |
```

Add a one-line cross-reference after the `### settings` section (before `### lifecycle`):

```
> Slot bindings for `ui_extension` plugins are declared in `ui-manifest.json` served by the plugin — not in `plugin.json`. See the `ui_extension` capability section below.
```

- [ ] **Step 3: Remove `### permissions` section (lines 115–119)**

Delete the entire block:

```
### `permissions`

Declared permission strings surfaced in the UI for user review before activation.
```

- [ ] **Step 4: Verify the `ui_extension` section (around line 239) already correctly names ui-manifest.json as authoritative**

The existing text reads: "Serve a UI manifest at `/ui-manifest.json` within your plugin's HTTP server." No change needed — the section already treats `ui-manifest.json` as the sole slot declaration source.

### 5b — plugin-sdk/README.md

- [ ] **Step 5: Remove `slots` and `permissions` rows from field table**

Delete the `slots` row (line 35 of README.md):
```
| `slots`        | UI slot bindings — `[{ "slot": "...", "priority": 0, "mode": "override"\|"extend" }]`. |
```

Delete the `permissions` row (line 38 of README.md):
```
| `permissions`  | Declared permission strings shown in the UI. |
```

- [ ] **Step 6: Fix the UI addon section — remove misleading plugin.json slots declaration**

The current text (lines 105–113) says "Declare the mapping in `plugin.json`:" and shows a `plugin.json` block with a `slots` array. Replace that paragraph and code block with:

```
The dashboard loads addon modules via the plugin proxy — the module URL is
`/api/plugins/{id}/proxy/<module-path>`. Slot bindings are declared exclusively in
`ui-manifest.json` served from your plugin's root:
```

The `ui-manifest.json` JSON block that follows (listing `slot` + `module` entries) stays unchanged.

### 5c — CHANGELOG.md

- [ ] **Step 7: Add a bullet to the existing `### Removed` section (line 185)**

Append after the existing bullet under `### Removed`:

```
- Inert `permissions` and `plugin.json slots[]` manifest fields removed from the `Descriptor` Go type and `plugin.schema.json`. Both were parsed but never enforced or consumed — a security-shaped field with no enforcement is misleading. Slot bindings for UI extensions are declared in `ui-manifest.json` (authoritative since SP4a); the `plugin.json` copy was a divergeable duplicate. Old manifests carrying these fields still load correctly (`additionalProperties: true`). The unused `Registry.AllWithCapability` method is also removed.
```

### 5d — Commit docs

- [ ] **Step 8: Run drift-guard test**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test -- --reporter=verbose src/plugin-sdk.test.ts
```

Expected: all 4 tests pass.

- [ ] **Step 9: Commit**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
git add docs/plugin-guide.md plugin-sdk/README.md CHANGELOG.md
git commit --no-gpg-sign -m "docs(plugin): remove slots/permissions references, clarify ui-manifest.json as slot SSOT"
```

---

## Final Verification

- [ ] **Full Go build**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go build ./...
```

Expected: no output (zero errors).

- [ ] **Plugin package test**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard/server
go test ./internal/plugin/ -v
```

Expected: 4 tests (ParsesV2, BackwardCompatV1, LegacySlotsAndPermissionsIgnored, plus any pre-existing registry/dispatcher tests) all pass.

- [ ] **Restore ent tree if accidentally regenerated**

```
git checkout -- /Users/alexanderwink/code/_privat/projects/agent-dashboard/server/internal/db/ent/
```

Only needed if you ran `go test ./...` rather than `go test ./internal/plugin/`.

- [ ] **Drift-guard TS test**

```
cd /Users/alexanderwink/code/_privat/projects/agent-dashboard
pnpm test -- --reporter=verbose src/plugin-sdk.test.ts
```

Expected: all 4 tests pass.

- [ ] **Symbol grep — confirm nothing lingers**

```
grep -rn "SlotBinding\|AllWithCapability" \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/server/ \
  /Users/alexanderwink/code/_privat/projects/agent-dashboard/src/
```

Expected: no output.
