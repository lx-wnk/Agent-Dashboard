# Provider Integration — Design Spec

**Date:** 2026-06-22
**Status:** Approved (design) — pending implementation plan
**Topic:** Pluggable, opt-in integration of additional agent/LLM providers (Codex, Gemini, Junie, Copilot, Cursor, Windsurf) and zero-cost handling for Ollama-backed local models.

---

## 1. Problem & Goals

The dashboard currently monitors three providers via five hardcoded switch statements (`sdk/types.go`, `parser/parser.go`, `parser/resolver.go`, `scanner/scanner.go`, `merger/merger.go`). Codex and Gemini are nominally supported but both route to a single stub parser (`ParseCodexSession`) that returns `CostUnknown=true` and extracts no tokens — Gemini is in fact mis-wired, parsed with a schema that does not match its files.

**Goals:**

1. Make adding a provider a **data change (a descriptor file), not a code change**, for any provider whose sessions are file-per-session JSONL.
2. Providers are **off by default**; a user opts in per provider via a Settings UI toggle, persisted in the DB.
3. Extract **real token usage, model, and cost** for the JSONL-family providers (replacing the `CostUnknown` stub).
4. Treat **Ollama-backed (local) models as `$0`**, not "unknown".
5. Keep the door open for IDE-embedded providers (Cursor, Copilot-in-VS-Code, Windsurf) via a Go adapter behind the same registry — **designed-for now, built later**.

**Non-goals (this spec):** the IDE-adapter implementations; a standalone Ollama server-health panel; out-of-process third-party provider plugins.

---

## 2. Feasibility Findings (research-backed)

Each provider was researched for what it persists locally. This table is the basis for the tiering in §7.

| Provider | Activity | Tokens | Model | Cost source | Local data |
|---|---|---|---|---|---|
| **Codex CLI** | ✅ | ✅ interactive / ⚠️ exec mode | ✅ | pricing table / `$0` if Ollama | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` |
| **Gemini CLI** | ✅ | ✅ | ✅ | pricing table / `$0` if Ollama | `~/.gemini/tmp/<projectHash>/chats/session-*.jsonl` |
| **Junie CLI** | ✅ | ✅ | ✅ | **in-file `cost` + `provider`** | `~/.junie/sessions/<id>/events.jsonl` |
| **Copilot CLI** | ✅ | ⚠️ aggregate at `session.shutdown` only | ✅ | unit-based | `~/.copilot/session-state/<id>/events.jsonl` |
| **Copilot (VS Code)** | ⚠️ file-mtime poll | ✅ per-turn | ✅ | unit-based | `…/Code/User/workspaceStorage/<hash>/chatSessions/*.jsonl` (mutation log) |
| **Cursor** | ⚠️ SQLite poll | ❌ always `0` locally | ✅ | — | `…/Cursor/User/**/state.vscdb` (`cursorDiskKV`) |
| **Windsurf** | ⚠️ (verify live) | ❌ likely (credit billing) | ⚠️ | — | `…/Windsurf/User/**/state.vscdb` (verify) |

**Key consequences for the design:**

- A naive `field → path` map is insufficient. The descriptor must express: **path fallback-lists** (Junie aliases token keys across versions), **token aggregation mode** (`cumulative` for Codex totals vs `perMessage` sum for Gemini), and an **event filter** (only some JSONL lines carry usage).
- The JSONL family (Codex, Gemini, Junie, Copilot-CLI) is fully declarative. The IDE family (Cursor, Copilot-VS-Code, Windsurf) is **not** — it needs SQLite-poll or mutation-log reconstruction and no distinct process to detect. → two source types behind one registry.
- **Cursor token counts are unobtainable locally** (`cursorDiskKV.tokenCount` is acknowledged-always-`0` by Cursor). Documented as infeasible, not deferred.

### Source field-paths (for the declarative parser)

**Codex CLI** — lines wrapped `{timestamp, type, payload}`:
- model: `payload.model` (on `type=="turn_context"` lines)
- provider: `payload.model_provider` (on `type=="session_meta"` lines)
- tokens (`mode: cumulative`, on `payload.type=="token_count"`): `payload.info.total_token_usage.{input_tokens,output_tokens,cached_input_tokens,reasoning_output_tokens}`
- caveat: exec-mode sessions emit no `token_count` → activity+model only.

**Gemini CLI** — `type=="gemini"` message lines:
- model: `model`
- tokens (`mode: perMessage`): `tokens.{input,output,cached,thoughts,tool}`
- caveat: older sessions are `.json` (whole-conversation) not `.jsonl`; handle/skip.

**Junie CLI** — `kind=="LlmResponseMetadataEvent"`, nested `event.agentEvent.modelUsage[]`:
- model: `modelUsage[].model`, provider: `modelUsage[].provider`, cost: `modelUsage[].cost`
- tokens (aliased — fallback-list): `inputTokens|input`, `outputTokens|output`, `cacheInputTokens|cacheReadInputTokens`, `cacheCreateTokens|cacheCreationInputTokens`, `reasoningTokens|thinkingTokens`

**Copilot CLI** — `events.jsonl`; `session.start`/`session.model_change` carry model; `session.shutdown.modelMetrics.<model>` carries per-model aggregate tokens/cost only (no per-turn).

---

## 3. Architecture

A single **provider registry** replaces the five hardcoded switches. A provider is one of:

- **Declarative descriptor** (`source: jsonl`) — built-in descriptors embedded via `go:embed`, plus a user override directory.
- **Registered Go adapter** (`source: custom:<id>`) — for IDE-embedded providers; an `Adapter` interface the registry resolves by id. None implemented this spec; the seam exists.

The registry is the single authority for the four per-provider operations the switches do today:

| Operation | Today (hardcoded) | After (registry-driven) |
|---|---|---|
| Process detection | `scanner.go:DetectProviderFromCommand` switch | match `ps` comm against each enabled provider's `exeNames` |
| Config-dir scan | `parser.go:AllAgentConfigDirs` appends | iterate enabled providers' `configDir` (env+default), skip if absent |
| Session parse | `resolver.go:resolverFor` switch | declarative engine (`source: jsonl`) or `Adapter.Parse` (`custom:*`) |
| Cost | `merger.go:EstimateCostForProvider` | `cost.rule` + `localIf` evaluation |

Data flow is unchanged in shape — scan → match PID → tail JSONL → read meta → merge → SSE broadcast — but each step becomes table-driven and gated by the provider's `enabled` flag.

`sdk.Provider` changes from a closed 3-const enum to an **open string** keyed by descriptor `id`. Existing consts (`ProviderClaude/Codex/Gemini`) remain as named constants for the built-ins.

### New package `server/internal/provider/`

- `descriptor.go` — the descriptor struct + YAML loader + validation.
- `registry.go` — load built-ins (`go:embed providers/*.yaml`) + user dir, merge DB `enabled` state, expose lookups (by exe, by id, enabled set).
- `jsonl_engine.go` — declarative parse: glob sessions, line-filter via `eventFilter`, resolve path-lists, apply token `mode`, map to `sdk.SessionData`.
- `adapter.go` — `Adapter` interface (`Detect`, `ConfigDirs`, `Parse`) for `source: custom:*`; registered in an internal map. No implementations this spec.
- `ollama.go` — cached `GET http://localhost:11434/api/tags` poll → local-model set; `localIf` evaluation.

---

## 4. Descriptor Schema

```yaml
id: codex                       # becomes sdk.Provider value
displayName: Codex CLI
enabled: false                  # default-off; DB enabled-state overrides at runtime
exeNames: [codex]               # ps comm match for process detection
configDir:
  env: CODEX_HOME               # optional env override
  default: ~/.codex
sessionGlob: sessions/**/rollout-*.jsonl   # relative to configDir
source: jsonl                   # or: custom:<adapterID>
parse:
  eventFilter:                  # optional; only matching lines carry usage
    path: payload.type
    equals: token_count
  tokens:
    mode: cumulative            # cumulative | perMessage
    input:     [payload.info.total_token_usage.input_tokens]   # fallback-list
    output:    [payload.info.total_token_usage.output_tokens]
    cacheRead: [payload.info.total_token_usage.cached_input_tokens]
  model:    [payload.model]
  provider: [payload.model_provider]
cost:
  rule: byModel                 # byModel (pricing table) | inFile (provider supplies cost) | unknown
  inFilePath: []                # for rule:inFile (e.g. Junie modelUsage[].cost)
  localIf:                      # → $0 + "local" badge
    providerEquals: ollama
    orModelInOllamaTags: true
```

**Field-path syntax:** dotted path with `[]` meaning "iterate array elements". Each field is a **list** of candidate paths tried in order (first non-null wins) to absorb schema drift.

**Token `mode`:** `cumulative` = the latest matching value is the session total (Codex). `perMessage` = sum across all matching lines (Gemini, Junie).

**User descriptors** live in `DASHBOARD_PROVIDER_DIR` (default e.g. `~/.config/agent-dashboard/providers/`); a user file with the same `id` overrides the built-in. Reading files only — no code execution — preserves the loopback/privacy posture.

---

## 5. Opt-in & Settings

- All providers default **off**. A provider is scanned only when its DB `enabled` row is true.
- New ent entity **`provider_setting`**: `id` (provider id, PK), `enabled` (bool), `ollama_zero_cost` (bool). Absent row ⇒ disabled.
- REST: `GET /api/providers` (list: id, displayName, detected-config-dir-present, enabled, capabilities from feasibility) and `PATCH /api/providers/{id}` (toggle). Follows existing task-API auth (Origin header, bearer where applicable).
- Vue **Settings → Providers** panel: one row per built-in/known provider with an on/off toggle, a "config dir detected/not found" hint, a capability badge (e.g. "tokens: full / activity only"), and a global "Ollama local models = $0" switch.

---

## 6. Cost & Ollama

- Add a cost state **`CostLocal`** (resolves to `$0`) distinct from the existing `CostUnknown`. The `Agent` card shows a **"local"** badge for `CostLocal`.
- `cost.localIf` resolves to `CostLocal` when: `provider == ollama` (explicit, e.g. Codex `model_provider`), OR the session model name (prefixes like `ollama_chat/` stripped, lowercased) is a member of the Ollama `/api/tags` set.
- Ollama classifier polls `GET http://localhost:11434/api/tags` once per refresh cycle, cached; gated by the global `ollama_zero_cost` switch. **Unreachable Ollama ⇒ skip local-classification**, fall back to `cost.rule`.
- `cost.rule: byModel` uses the existing pricing table (`parser/pricing.go`); a non-Claude model with no pricing entry ⇒ `CostUnknown` (unchanged gate). `cost.rule: inFile` (Junie) reads cost straight from the session and never consults the pricing table.

---

## 7. Scope & Tiers

**In scope (this spec):**

- T1 — fully declarative, full data: **Codex CLI** (upgrade from stub), **Gemini CLI** (fix mis-wiring + real parse), **Junie CLI** (new, `source: jsonl`, in-file cost).
- Registry + descriptor engine + `provider_setting` table + Settings UI + Ollama `$0`.
- **Copilot CLI** as a stretch (T2): activity + model declaratively; tokens from `session.shutdown` aggregate (no per-turn).

**Out of scope — follow-on spec, registry built to accept (`source: custom:*`):**

- **Cursor adapter** — model + activity via `cursorDiskKV` SQLite poll. **Tokens infeasible** (always `0` locally) — documented, not promised.
- **Copilot-in-VS-Code adapter** — per-turn tokens via `chatSessions` JSONL mutation-log reconstruction (kinds 0/1/2/3).
- **Windsurf** — verify live `state.vscdb` first; expected model+activity, tokens likely infeasible (credit billing).
- **Junie IDE plugin** — no local artifact; infeasible. (Junie **CLI** is in scope.)

---

## 8. Error Handling

- **Descriptor validation at load**: malformed descriptor (bad path syntax, unknown `mode`/`source`) ⇒ that provider is disabled and logged; the scan never crashes. (Contrast the past jsonschema-tag panic that silently disabled *all* channel agents.)
- **Schema drift**: path fallback-lists absorb minor changes; if every candidate path misses, the field is omitted (tokens dropped ⇒ `CostUnknown`), never a hard error.
- **Exec-mode Codex** (no `token_count` lines): activity + model only, `CostUnknown`.
- **Gemini legacy `.json`** sessions: detected and skipped (or minimally handled), not parsed as JSONL.
- **Ollama unreachable**: local-classification skipped; cost falls back to `rule`.

---

## 9. Testing

- **Table-driven descriptor-parse tests** with captured **real** JSONL fixtures per provider (Codex `token_count` line, Gemini `gemini` message line, Junie `LlmResponseMetadataEvent`, Copilot `session.shutdown`) asserting extracted tokens/model/provider/cost.
- **Registry tests**: built-in load, user-dir override by id, disabled-by-default, enabled gating.
- **Ollama classifier test**: faked `/api/tags` response → `CostLocal`; unreachable → fallback.
- **Cost tests**: `byModel` with/without pricing, `inFile` (Junie), `localIf` precedence.
- Spawn-free, per the DI-seam convention (`go test` runs freely; no real agent spawn).
- ent regen on every `task test` (FeatureUpsert retained) for the new `provider_setting` entity.

---

## 10. Files Touched (orientation, not exhaustive)

**New:** `server/internal/provider/{descriptor,registry,jsonl_engine,adapter,ollama}.go`, `server/internal/provider/providers/{codex,gemini,junie,copilot-cli}.yaml`, ent `provider_setting` schema, Vue `Settings/Providers` panel + store.

**Modified:** `sdk/types.go` (open Provider string + `CostLocal`), `parser/parser.go` (`AllAgentConfigDirs` → registry), `parser/resolver.go` (→ engine/adapter dispatch), `scanner/scanner.go` (`DetectProviderFromCommand` → registry exe match), `merger/merger.go` (cost via `rule`+`localIf`), API router (providers endpoints), `config/config.go` (`DASHBOARD_PROVIDER_DIR`).

**Docs (same change):** `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `PRIVACY.md` (new local paths read), plus `.agent-context` memory/decisions per project convention.
