# Anthropic Claude API Spawner — Design Spec

**Date:** 2026-06-23
**Status:** Approved (design) — pending implementation plan
**Topic:** Let pipeline stage agents and refinement chat run directly against the Anthropic Messages API (Claude API), via an out-of-process spawner binary using the official Go SDK — without pulling the SDK into the main server.

---

## 1. Problem & Goals

Today the dashboard's `llmadapter` package runs stage agents via four `adapter_type`s:

- `claude` — the **`claude` CLI subprocess** (`exec.Command("claude", …)`), not the API.
- `openai` — hand-rolled HTTP client to an OpenAI-compatible `/v1/chat/completions`.
- `ollama` — hand-rolled HTTP client to `/api/chat`.
- `custom` — arbitrary binary, `LLMSpawnArgs` JSON on stdin / `LLMSpawnResult` JSON (or token-lines) on stdout.

There is **no way to run a stage agent against the Anthropic Messages API directly** (with an `ANTHROPIC_API_KEY`, no Claude CLI). Anthropic's API is **not** OpenAI-compatible — different endpoint (`POST /v1/messages`), auth (`x-api-key` + `anthropic-version: 2023-06-01`), request shape (top-level `system`, content blocks, required `max_tokens`), and SSE event shape (`content_block_delta`/`text_delta`). So it cannot be the OpenAI adapter pointed at a new base URL.

**Goals:**

1. Add a first-class **`anthropic`** spawner that calls the Messages API directly.
2. Use the **official Go SDK** (`github.com/anthropics/anthropic-sdk-go`) for correctness (SSE, retries, model quirks).
3. Keep the SDK **out of the main server binary** (`server/go.mod` and the Go workspace) — preserve symmetry with the hand-rolled OpenAI/Ollama adapters and avoid a heavy dependency in the core.
4. Support **both** execution paths: non-streaming `Spawn` (pipeline stages) and streaming `SpawnStream` (refinement chat).

**Non-goals:** the formal plugin registry; OpenAI-compat shims; Managed Agents / tool use / computer use (text generation only); mapping Anthropic token usage into the pipeline cost/budget path (follow-up).

---

## 2. Why not the plugin registry

The repo has an out-of-process **plugin registry** (`server/internal/plugin/`, capabilities `auth_provider`/`route_extension`/`ui_extension`, loopback HTTP services with health-check + restart-backoff supervision). It is **the wrong host** here:

- It is **request/response only** — no SSE/streaming path. Refinement chat (`/api/refine`) requires `SpawnStream`; a plugin would have to buffer the whole response, breaking live token output.
- Its capability model is auth/route/UI — there is no `spawner`/`llm_adapter` capability, and adding one plus an HTTP+SSE protocol is far more surface than needed.

The **`custom` exec adapter is the correct seam**: it already `exec`s a separate binary and already implements **both** `Spawn` and `SpawnStream` (`server/internal/llmadapter/llm_custom.go`). We reuse it.

---

## 3. Architecture

A standalone Go binary, `plugins/anthropic-spawner/`, **with its own `go.mod`/`go.sum`** (built `GOWORK=off`, exactly how the existing plugin binaries isolate dependencies). It imports `anthropic-sdk-go`. The main server never imports the SDK.

It is wired in through the existing `custom` exec contract, surfaced as a new first-class `adapter_type: "anthropic"`.

```
pipeline stage handler / refine chat
  → llmadapter.NewLLMSpawnerFromSpawner(spawner row)        [adapter_factory.go]
  → case "anthropic": CustomCommandSpawner{Command: <resolved anthropic-spawner path>}
  → exec binary, LLMSpawnArgs JSON on stdin
        binary: ANTHROPIC_API_KEY (+ optional ANTHROPIC_BASE_URL) from its own env
                → Messages API via anthropic-sdk-go
        ├─ Stream=false (Spawn):       write synthetic JSONL to WorkDir;
        │                              print LLMSpawnResult{SessionFile,SessionID} JSON on stdout
        └─ Stream=true  (SpawnStream): print each text_delta as one stdout line
```

**Net server-side footprint:** one new struct field (`LLMSpawnArgs.Stream`), two one-line setters in the custom adapter, one `adapter_factory` case + a path resolver. Everything Anthropic-specific is quarantined in the binary's own module.

### Components

| Unit | Responsibility | Depends on |
|---|---|---|
| `plugins/anthropic-spawner/main.go` (+ own go.mod) | Read `LLMSpawnArgs` from stdin; call Messages API via SDK; emit synthetic JSONL (Spawn) or token-lines (SpawnStream); handle `refusal` | `anthropic-sdk-go`, the shared `LLMSpawnArgs`/`LLMSpawnResult` JSON shapes (copied as local structs — no cross-module import) |
| `llmadapter.LLMSpawnArgs.Stream bool` | Signal which mode the custom binary should run in | — |
| `CustomCommandSpawner.Spawn`/`SpawnStream` | Set `args.Stream = false`/`true` before marshaling | existing |
| `adapter_factory.go` `case "anthropic"` | Return `CustomCommandSpawner{Command: resolveAnthropicSpawnerPath()}` | config/env |
| `resolveAnthropicSpawnerPath()` | `DASHBOARD_ANTHROPIC_SPAWNER_CMD` → default `plugins/anthropic-spawner/anthropic-spawner` → PATH lookup | `config` |

---

## 4. The `Stream` field (the one core wrinkle)

`CustomCommandSpawner.Spawn` and `.SpawnStream` `exec` the **same** command but expect different stdout: `Spawn` parses all stdout as one `LLMSpawnResult` JSON (`cmd.Output()` + `json.Unmarshal`); `SpawnStream` reads stdout line-by-line, emitting each as a chunk. `LLMSpawnArgs` currently carries **no mode signal**, so a single binary cannot tell which contract to honor.

**Fix:** add `Stream bool` to `LLMSpawnArgs` (`server/internal/llmadapter/llm_spawner.go`). `Spawn` sets it `false`; `SpawnStream` sets it `true`. The binary branches on `args.Stream`. This is a small, generally-useful enhancement — it disambiguates dual-mode for **any** custom binary, not just this one. Existing custom binaries that ignore the field keep working (they already only implement one mode in practice).

---

## 5. The binary

Reads `LLMSpawnArgs` JSON from stdin. Fields used: `SystemPrompt`, `UserPrompt`, `Model`, `WorkDir`, `StageRunID`, `Stream`. Builds a Messages request:

- `system` = `SystemPrompt`; `messages` = one `user` message with `UserPrompt`.
- `model` = `Model` if set, else **`claude-opus-4-8`** (skill default).
- `thinking: {type: "adaptive"}`, `output_config.effort: "high"`.
- `max_tokens`: `16000` non-stream, `64000` streaming (skill defaults; streaming avoids HTTP timeouts on large output).
- API key resolved by the SDK from `ANTHROPIC_API_KEY` (inherited from the server's env — `llm_custom.go` does not reset `cmd.Env`); optional `ANTHROPIC_BASE_URL` honored for testing/proxies.

**Non-stream (`Stream=false`):** call `client.Messages.New(...)`. Concatenate `text` blocks. Write a synthetic session file under `WorkDir` matching the existing adapter shape (one JSON object + newline):

```json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"<output>"}],"timestamp":"<RFC3339>"}}
```

Print `{"pid":0,"sessionId":"anthropic-<StageRunID>","sessionFile":"<path>"}` (the `LLMSpawnResult` JSON) to stdout — and nothing else on stdout.

**Stream (`Stream=true`):** call `client.Messages.NewStreaming(...)`; for each `content_block_delta` of type `text_delta`, print the delta text as one stdout line. No session file needed (the refine path consumes the channel; it does not read a `SessionFile`).

**Refusal:** if `stop_reason == "refusal"`, write the `stop_details` explanation to **stderr** and exit non-zero. The custom adapter wraps non-zero exit + stderr into an error, surfacing as a stage failure (Spawn) or an `[ERROR] …` line (SpawnStream).

**Errors:** any SDK/transport error → stderr + non-zero exit (same handling).

---

## 6. Surfacing — `adapter_type: "anthropic"`

`adapter_factory.go` gains:

```go
case "anthropic":
    return &CustomCommandSpawner{Command: resolveAnthropicSpawnerPath(cfg)}, nil
```

The spawner DB row stores `adapter_type=anthropic` and an optional `model_override`; the existing model precedence (`spawner.ModelOverride` → `task.Metadata["model"]` → per-stage default) is untouched. `adapter_config` is **not** used (consistent with `custom`); the API key and base URL come from the server's environment, inherited by the exec'd binary.

`resolveAnthropicSpawnerPath`: `DASHBOARD_ANTHROPIC_SPAWNER_CMD` if set; else a default relative path (`plugins/anthropic-spawner/anthropic-spawner`); else PATH lookup (`exec.LookPath`). If unresolved, the factory returns a clear error so a misconfigured deployment fails loudly rather than silently.

This makes "Claude API" a first-class, selectable spawner type (UI/spawner config) without the user hand-entering a binary path, while keeping the SDK out-of-process.

---

## 7. Build / Release / CI

`plugins/anthropic-spawner/` ships `main.go`, `main_test.go`, `go.mod`, `go.sum` (with `anthropic-sdk-go`). Per the project's plugin-CI conventions, add it to `.github/workflows/ci.yml` in **all** the relevant spots:

1. `test-plugins` job matrix,
2. `lint-plugins` job matrix,
3. `security` (govulncheck) matrix,
4. the "Build plugin binaries" loop.

Plus a valid **golangci v2** config for the module (`linters.exclusions.rules`, not `issues.exclude-rules`) and a `.testcoverage.yml` exclusion. The module's `go.sum` must be valid/complete. No `plugin.json` — this is **not** a registry plugin; it is a custom-exec binary that happens to live under `plugins/` for dependency isolation. Optionally add a `build:spawners` Taskfile target so local builds can produce the binary (plugins are otherwise CI-only).

---

## 8. Error Handling

- **Missing `ANTHROPIC_API_KEY`:** binary exits non-zero with a clear stderr message → surfaces as a stage/refine error (not a silent empty response).
- **Unresolved binary path:** `resolveAnthropicSpawnerPath` returns an error from the factory → spawner construction fails loudly.
- **Refusal / API error / transport error:** stderr + non-zero exit, wrapped by the custom adapter.
- **Streaming for a non-streaming consumer / vice versa:** the `Stream` flag guarantees the binary emits the contract the caller expects; mismatch is impossible by construction.
- **Model rejects params:** the SDK surfaces 400s (e.g. `budget_tokens` on a 4.x model) — the binary uses adaptive thinking + effort only (no `budget_tokens`, no sampling params), per the current API surface, avoiding those 400s.

---

## 9. Testing

- **Binary (`plugins/anthropic-spawner/main_test.go`):** construct the SDK client against an `httptest` server via `ANTHROPIC_BASE_URL`; feed a canned Messages response. Assert: (a) non-stream writes the exact synthetic-JSONL shape + prints a valid `LLMSpawnResult`; (b) streaming prints one line per `text_delta`; (c) `refusal` → non-zero exit + stderr; (d) default model is `claude-opus-4-8` when `Model` is empty.
- **Server (`llmadapter`):** `LLMSpawnArgs.Stream` is set `false` by `Spawn` and `true` by `SpawnStream` (table test with a fake echo binary that reports the received `Stream` value); `adapter_factory` `case "anthropic"` resolves the configured path and errors clearly when unresolved.
- Spawn-free, per the DI-seam convention; `go test ./...` runs without contacting the real API.

---

## 10. Files Touched (orientation)

**New:** `plugins/anthropic-spawner/{main.go,main_test.go,go.mod,go.sum,.golangci.yml,.testcoverage.yml}`, CI matrix entries.

**Modified:** `server/internal/llmadapter/llm_spawner.go` (`Stream` field), `server/internal/llmadapter/llm_custom.go` (set `Stream` in `Spawn`/`SpawnStream`), `server/internal/llmadapter/adapter_factory.go` (`case "anthropic"` + `resolveAnthropicSpawnerPath`), `server/internal/config/config.go` (`AnthropicSpawnerCmd` / `DASHBOARD_ANTHROPIC_SPAWNER_CMD`), `.github/workflows/ci.yml`.

**Docs (same change):** `README.md` (new spawner type + `ANTHROPIC_API_KEY`), `CHANGELOG.md`, `CONTRIBUTING.md` (how the out-of-process spawner works), `PRIVACY.md` (§3 "LLM adapters" — add Anthropic: stage-agent prompts sent to `api.anthropic.com`, US/DPF transfer basis), plus `.agent-context` decisions/log.

---

## Open question for spec review

- **Model default:** spec uses `claude-opus-4-8`. If the dashboard's pipeline has its own default-model policy (e.g. cost-tuned stages prefer Sonnet/Haiku), the binary should still honor `Model` from precedence; the hard-coded default only applies when nothing is set. Confirm `claude-opus-4-8` is the right "nothing set" fallback, or pick a cheaper default for stage agents.
