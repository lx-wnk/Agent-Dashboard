# Plugin SP6 — Settings Delivery & Validation — Design Spec

> Date: 2026-06-29 · Status: Draft for review · Branch: `feat/plugin-sp6-settings-delivery` (off `feat/plugin-followups` / `upcoming`)
> Follow-up to the SP1–SP5 plugin redesign. Closes the headline functional gap found in the post-integration audit: per-plugin settings are stored + encrypted but never delivered to plugin processes.

## Why

SP1 built per-plugin settings storage (`plugin_setting`, AES-GCM secrets) and SP4b built the settings UI. But `pluginsettings.Service.Decrypted` has **zero production callers** — the registry only injects an OS-env allow-list (`buildPluginEnv(desc.Env)`). So a value entered in the settings UI (e.g. an API key) never reaches the plugin process: the whole feature (D4 of the parent spec) is non-functional end to end. Additionally, `Service.Put` stores every value as an opaque string with no validation against the declared `SettingField.Type`.

## Scope

In: inject decrypted plugin settings into the plugin subprocess env at spawn (`PLUGIN_SETTING_<KEY>`); a settings-provider seam so the registry can read decrypted values without importing `pluginsettings`; server-side value validation in `Service.Put` against `SettingField.Type`; `url` field type handling on the frontend.

Out: live settings reload without restart (env is read at process start; changing a setting requires deactivate/activate or restart — document); HTTP settings-callback mechanism (rejected). DASHBOARD_* secret blocklist in `buildPluginEnv` is **SP8** (security) — SP6 assumes it.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Deliver settings as **env vars `PLUGIN_SETTING_<KEY_UPPER>`** at process spawn | Consistent with the existing `buildPluginEnv` mechanism; language-agnostic (`os.Getenv`); the `PLUGIN_SETTING_` prefix avoids collision with system env. Key uppercased + non-alphanumeric → `_`. |
| D2 | Registry gets a **`SettingsProvider` seam** (`func(ctx, id) (map[string]string, error)`) injected via DI; default nil = no settings | Keeps `plugin` a leaf package (no import of `pluginsettings`/ent); nil-safe for the no-DB path. The provider returns DECRYPTED values (secrets included). |
| D3 | Settings are injected at **every start** (initial `Load`, `StartOne`, restart, transient) | A restarted/re-activated plugin must get current settings. Read fresh from the provider each spawn. |
| D4 | **Server-side validation** in `Service.Put` by `SettingField.Type` | `int` must parse as integer; `bool` ∈ {true,false}; `url` must parse with a scheme+host; `enum` ∈ declared values; `string` any. Invalid → `ErrInvalidValue` → 400. Needs the manifest schema at Put time (the controller already loads the descriptor for GetSettings). |

## Architecture

### Settings injection (`server/internal/plugin/registry.go`)
- Add a `settings SettingsProvider` field to `Registry` (set via a new `Hooks.Settings` callback or a `SetSettingsProvider`). Type: `type SettingsProvider func(ctx context.Context, id string) (map[string]string, error)`.
- In `startEntry` (and the restart spawn in `watchPlugin`), after building the base env, if a provider is set, fetch `provider(ctx, desc.ID)`; for each `key,val` add `PLUGIN_SETTING_<sanitize(key)>=val` to the process env. `sanitize` = uppercase + `[^A-Z0-9_]`→`_`. A provider error logs a warning and starts the plugin WITHOUT settings (don't block startup on a settings read failure) — surface via slog.
- `buildPluginEnv` stays for the OS-allowlist; the settings vars are appended after it (settings can't be overridden by the allow-list since distinct prefix).

### Provider wiring (`server/cmd/serve/di.go`)
- Build the provider from `pluginSettingsSvc` (which already exists) wrapping a `Decrypted(ctx, id) (map[string]string, error)` call (the decrypt path that returns plaintext for injection — `pluginsettings.Service.Decrypted`). Inject into the registry before `Load`.
- Provider nil when `entClient == nil`.

### Validation (`server/internal/pluginsettings/service.go`)
- `Put(ctx, id, values, schema)` — the service needs the field schema to validate. Either pass the `[]plugin.SettingField` schema into `Put` (controller has it) or have the service look it up. Decision: **pass the schema in** (controller already resolves the descriptor). Validate each submitted key: must exist in schema; value must satisfy its `Type`. Add `ErrInvalidValue` sentinel; the handler's `classify` maps it (alongside `ErrUnknownKey`) to 400.
- `url`: parse with `net/url.ParseRequestURI` requiring a scheme. `int`: `strconv.Atoi`. `bool`: `"true"|"false"`. `enum`: membership in `SettingField.Enum`.

### Frontend (`src/components/PluginSettingsForm.vue`)
- `url` field → `<input type="url">` (currently falls through to text). Keep the existing `int` `type="text" inputmode="numeric"` (SP4b fix). No other change.

## Data flow
```
activate/start plugin → registry.startEntry
  → base = buildPluginEnv(desc.Env)
  → if settingsProvider: vals = provider(ctx, id)  # decrypted
       for k,v: base += "PLUGIN_SETTING_"+UPPER(k)+"="+v
  → exec.Command(... Env: base ...)
plugin process: os.Getenv("PLUGIN_SETTING_APIKEY")

PUT /api/plugins/{id}/settings → controller (resolves schema) → Service.Put(id, values, schema)
  → per field: validate value vs Type → ErrInvalidValue (400) on fail → encrypt+persist
```

## Error handling
- Provider read failure at spawn → log warning, start without settings (availability over completeness).
- `Put` validation failure → 400 with the offending key/type; nothing persisted (validate all before writing).
- Secret values DO land in the plugin process env (plaintext) — acceptable: the plugin is operator-installed code running as the same user (can already read the DB + key file). Documented. The DASHBOARD_* blocklist (SP8) prevents dashboard's own secrets leaking.

## Testing
- Injection: a spawned real-subprocess plugin (reuse SP2 fixture) with a seeded `plugin_setting` sees `PLUGIN_SETTING_<KEY>` in its env (plugin writes its env to a file the test reads). Secret value decrypted correctly. No provider → no PLUGIN_SETTING_ vars.
- Sanitize: `api-key` → `PLUGIN_SETTING_API_KEY`.
- Validation: `Put` rejects non-int for `int`, bad url for `url`, out-of-set for `enum`, non-bool for `bool`; accepts valid; unknown key still 400; masked-sentinel secret still skipped (SP4b behavior preserved).
- Frontend: `url` field renders `type="url"`.

## Risks / notes
- ent: no schema change.
- Changing a setting takes effect on next plugin (re)start — document in the guide (live-reload is out of scope).
- `Service.Put` signature change (add schema param) → update the one caller (controller) + tests.
