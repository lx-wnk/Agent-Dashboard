# AgenticOS A1 + S2 — Obsidian Slice and Effort Tuning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the kernel against one real Application end to end — registry entry, capability declaration, grants, gated calls, memory writes — and prove it against an **irreversible** action, because a gate designed against a harmless case is a gate designed against assumptions.

**Architecture:** Obsidian is an in-server `reach` application registered in the resource registry. Its four operations are capabilities, two of them irreversible. It talks to a local REST API over HTTPS with a self-signed certificate, so the TLS decision is explicit rather than a silent `-k`. Effort tuning rides along in the same stage: it is unrelated, small, and used daily.

**Tech Stack:** Go 1.26, ent v0.14.6, chi, SQLite. Vue 3 for the settings panel and the effort control.

**Spec:** `docs/superpowers/specs/2026-08-27-agenticos-obsidian-slice-design.md`
Umbrella: `docs/superpowers/specs/2026-08-27-agenticos-overview-design.md`

**Depends on:** K1 (registry), K2 (capabilities and grants), K3 (memory store). Do not start until all three are merged.

> **Verify before dispatching any task that consumes K2 or K3.** This plan was written before either existed, and every symbol it names from them is an assumption. K1's execution produced five plan defects, every one an unverified claim about code. Tasks 3, 5, 6 and 7 touch K2 or K3 — re-read the merged signatures and correct the brief before dispatching.

## Global Constraints

- **Go workspace** with `./sdk` and `./server`; `go build ./...` from the root FAILS — use `go build ./server/...`.
- **`go test` regenerates the ent tree.** Keep `git status --short server/internal/db/ent/` empty before committing unless the regeneration is the change.
- **Never amend a commit.**
- **New unique indexes are pre-created under ent's exact generated name.**
- **Frontend gates:** `pnpm lint && pnpm typecheck && pnpm test` for any task touching `src/`.
- **`pnpm build` deletes `server/frontend/dist/.gitkeep`**, which `//go:embed all:dist` needs to compile without a frontend build. Restore it with `git checkout HEAD -- server/frontend/dist/.gitkeep` before committing.
- **Everything that ships is English.** Conventional Commits, no task or phase references.
- **Secrets never appear in a response body, a log line, or an error message.** Assert this against the store and the wire, not against the UI.

## What exists today, verified at plan time

| Symbol | Location | Shape |
|---|---|---|
| `validation.IsBlockedIP` | `server/internal/validation/ssrf.go:28` | `func(ip net.IP) bool` |
| `validation.IsBlockedHost` | `ssrf.go:40` | `func(host string) bool` |
| `validation.SafeDialContext` | `ssrf.go:52` | `func(ctx, network, addr string) (net.Conn, error)` |
| `services.SpawnerResolver` | `server/internal/services/spawner_resolver.go:54` | interface with `Resolve(ctx, taskID, stage string) (*ent.Spawner, SpawnerSource, error)` |
| `services.NewSpawnerResolver` | `spawner_resolver.go:66` | `func(tasks, projects, spawners, pcfg) SpawnerResolver` |
| `apispawners.ValidAdapterTypes` | `server/internal/api/spawners/validation.go:21` | `[]string{"claude", "ollama", "openai", "custom", "acp"}` |
| `spawner.adapter_config` | `server/internal/db/ent/schema/spawner.go:31-33` | `JSON map[string]string` with a raw `entsql.Default("{}")` |
| Encrypted plugin settings | `server/internal/plugin/settings_service.go` | AES-GCM ciphertext plus nonce; `MaskedSentinel = "********"`, which also means "leave unchanged" on write |

---

### Task 1: Effort in the spawner adapter config

Independent of everything else in this plan. Doing it first gets a daily-use improvement in early and keeps it out of the slice's review surface.

**Files:**
- Create: `server/internal/services/effort_resolver.go`
- Test: `server/internal/services/effort_resolver_test.go`

**Interfaces:**
- Consumes: `services.SpawnerResolver`, `ent.Spawner.AdapterConfig`.
- Produces: `services.ResolveEffort(ctx context.Context, r SpawnerResolver, taskID, stage string) (effort string, source SpawnerSource, supported bool, err error)`; the key constant `services.AdapterConfigEffortKey = "effort"`.

**Effort lives in `adapter_config`, not on `task`.** `adapter_config` is already a `map[string]string` with the SQL-default handling SQLite needs, and the resolution chain — task → project stage → project → global stage → default — already exists and already reports which tier won. "Effort" is a Claude concept; `adapter_type` spans five values, and a column on `task` would force a universal meaning onto something that is not universal.

- [ ] **Step 1: Write the failing test**

```go
func TestResolveEffortReadsAdapterConfig(t *testing.T) {
	// Seed a spawner whose adapter_config carries {"effort": "high"} and a task
	// pointing at it. Assert ResolveEffort returns "high" and the source the
	// spawner resolver reported, so the UI can show which tier won.
}

func TestResolveEffortUnsupportedAdapterIsVisible(t *testing.T) {
	// Seed a spawner with adapter_type "ollama" and an effort value.
	// Assert supported == false.
	//
	// The setting must be visibly inapplicable rather than silently dropped —
	// the same treatment a provider without a skill format gets from the
	// materializer.
}

func TestResolveEffortAbsentIsNotAnError(t *testing.T) {
	// No effort key at all: effort == "", supported per adapter, err == nil.
	// An unset optional setting is not a failure.
}
```

Write all three out against the `services` package's existing test helpers.

- [ ] **Step 2: Run red**

Run: `cd server && go test ./internal/services/ -run TestResolveEffort -count=1`
Expected: FAIL — `undefined: services.ResolveEffort`.

- [ ] **Step 3: Implement**

Which adapter types understand effort is a declared list in this file, not a guess at call sites. Start with `claude` and `anthropic`; everything else reports `supported == false`.

- [ ] **Step 4: Verify and commit**

```bash
cd server && go test -race -count=1 ./internal/services/
git commit -m "feat(services): resolve reasoning effort through the spawner chain

Effort lives in adapter_config rather than on the task: adapter_config is
already a string map with the SQL default SQLite needs, and the resolution
chain already reports which tier won.

An adapter that does not understand effort reports the setting as
inapplicable rather than dropping it silently — the same treatment a provider
without a skill format gets."
```

---

### Task 2: Effort in the UI

**Files:**
- Modify: `src/features/settings/components/SpawnerDetailView.vue`
- Modify: `src/utils/models.ts` or a sibling — the effort option list, as a shared constant
- Test: `src/features/settings/components/__tests__/SpawnerDetailView.test.ts`

**Interfaces:**
- Produces: `EFFORT_OPTIONS` in the shared-constants location the project's SSOT table names for client option lists.

The control sits next to the model control and shows the **resolved** value with its source, because the resolver already reports which tier won and surfacing that is most of the value. An adapter that does not support effort shows the control disabled with the reason, not hidden.

- [ ] **Step 1: Write the failing component test**

Assert: the control renders; selecting a value emits the expected update; an unsupported adapter renders it disabled with a visible reason rather than omitting it.

- [ ] **Step 2: Run red, implement, run `pnpm lint && pnpm typecheck && pnpm test`**

- [ ] **Step 3: Commit**, restoring `server/frontend/dist/.gitkeep` first if a build ran.

---

### Task 3: The Obsidian application resource

*(Consumes K1 and K2 — verify both before dispatch.)*

**Files:**
- Create: `server/internal/apps/obsidian/app.go` — registry registration and capability declaration
- Test: `server/internal/apps/obsidian/app_test.go`

**Interfaces:**
- Consumes: `repo.ResourceRepo`, `repo.ResourceKindApplication`, `repo.CapabilityRepo` *(K2 — verify)*.
- Produces: `obsidian.Register(ctx, resources repo.ResourceRepo, caps repo.CapabilityRepo) error`.

Capabilities, per spec §4.2:

| Capability | Class | Reversible | Enforceable by |
|---|---|---|---|
| `obsidian.read` | reach | — | server |
| `obsidian.search` | reach | — | server |
| `obsidian.write` | reach | **no** | server |
| `obsidian.delete` | reach | **no** | server |

`obsidian.write` is marked irreversible deliberately and not as pedantry: writing over an existing note destroys its content as thoroughly as deleting it, and a model that guarded only `delete` would give false comfort.

**Implementation shape is a property, not a definition.** This is an in-server module, not a subprocess plugin: Obsidian's REST API is on the same machine, so a subprocess would add a hop without adding isolation. Third-party applications keep the existing plugin mechanism. The registry entry is what makes this an Application.

- [ ] **Step 1: Write the failing test**

```go
func TestRegisterDeclaresIrreversibleWriteAndDelete(t *testing.T) {
	// After Register, assert obsidian.write and obsidian.delete both carry
	// reversible == false.
	//
	// This is the capability the whole slice exists to exercise: an
	// irreversible capability never auto-grants from a preset alone.
}

func TestRegisterIsIdempotent(t *testing.T) {
	// Register twice; assert exactly one application resource and four
	// capabilities exist. Boot runs this every time.
}
```

- [ ] **Step 2: Run red, implement, verify, commit**

---

### Task 4: The Obsidian client, with the TLS decision made explicit

**Files:**
- Create: `server/internal/apps/obsidian/client.go`
- Test: `server/internal/apps/obsidian/client_test.go`

**Interfaces:**
- Produces: `obsidian.Client` with `Read`, `Search`, `Write`, `Delete`; `obsidian.Config{BaseURL, APIKey, VaultRoot, TLSMode string}`; constants `obsidian.TLSVerify`, `TLSPinned`, `TLSInsecureLoopback`.

**Two problems this task must not paper over.**

**TLS.** Obsidian's Local REST API serves HTTPS with a self-signed certificate, and the common workaround is `curl -k` — verification off. Shipping that silently would put a permanently unverified TLS client into a system whose entire posture is "local-first, nothing leaves the machine". Three modes, defaulting to the safest that works:

- `verify` — normal verification; works if the user trusted the certificate
- `pinned` — **the default**; pin the fingerprint, captured once on first connect and shown to the user for confirmation
- `insecure-loopback` — verification off, permitted **only** when the host resolves to loopback, surfaced in the UI as a warning rather than buried in config

**SSRF.** `validation.IsBlockedIP` rejects loopback, and Obsidian's API is *on* loopback, so the existing guard would refuse it. **Do not relax the guard.** Loosening it globally would weaken every outbound call in the system to accommodate one application. Instead this client uses its own dial policy permitting exactly one host — the configured one, resolved once, pinned to that address — and that policy is a property of the application's registry entry, visible and revocable.

This is the same shape as the capability model itself: a narrow, named, inspectable exception rather than a widened default.

- [ ] **Step 1: Write the failing tests**

```go
func TestInsecureLoopbackRefusedForPublicHost(t *testing.T) {
	// Config{TLSMode: insecure-loopback, BaseURL: "https://example.com"}.
	// Assert construction fails at validation, not at connect time — a
	// misconfiguration that only surfaces on first use is one that ships.
}

func TestPinnedRefusesAChangedFingerprint(t *testing.T) {
	// Start an httptest TLS server, pin its fingerprint, then restart it with
	// a fresh certificate and assert the client refuses.
	//
	// On loopback a changed certificate is usually a reinstall — but the user
	// confirms it, the system does not assume it.
}

func TestDialPolicyRefusesAnyOtherHost() {
	// The client's dialer must refuse a host other than the configured one,
	// including after a DNS answer changes between resolve and connect.
}

func TestVaultPathContainmentRefusesEscape(t *testing.T) {
	// A note path escaping VaultRoot is refused before any request is built.
	// The root is a boundary, not a suggestion.
}

func TestAPIKeyNeverAppearsInAnError(t *testing.T) {
	// Force a request failure and assert the key appears in neither the error
	// text nor any header the test can observe.
}
```

Write all five out. The third and fifth are the ones a rushed implementation gets wrong.

- [ ] **Step 2: Run red, implement, verify, commit**

```bash
git commit -m "feat(obsidian): add the vault client with an explicit TLS decision

The Local REST API serves a self-signed certificate and the usual workaround
is disabling verification entirely. Three modes make that a decision instead
of a default: verify, pinned (the default, no certificate install required),
and insecure-loopback which is refused for any non-loopback host.

The SSRF guard is not relaxed. This client carries its own dial policy
permitting exactly the configured host, resolved once and pinned, rather than
widening a check every outbound call in the system depends on."
```

---

### Task 5: Gated operations

*(Consumes K2 — verify the Decider and enforcer signatures before dispatch.)*

**Files:**
- Create: `server/internal/apps/obsidian/service.go`
- Test: `server/internal/apps/obsidian/service_test.go`

**Interfaces:**
- Consumes: `capability.ServerEnforcer`, `capability.Decide` *(K2 — verify)*.
- Produces: `obsidian.Service` wrapping `Client`, with every method gated.

- [ ] **Step 1: Write the failing tests**

```go
func TestUngrantedDeleteNeverReachesTheClient(t *testing.T) {
	// Inject a client that records calls. With no grant for obsidian.delete,
	// assert Delete returns a denial AND the recording client saw nothing.
	//
	// Asserting only on the error would pass an implementation that deletes
	// first and reports afterwards.
}

func TestPresetAloneDoesNotSatisfyAnIrreversibleCapability(t *testing.T) {
	// A project-scoped standing grant must not by itself authorise
	// obsidian.delete. Irreversible capabilities require an explicit scoped
	// grant.
}

func TestExpiredGrantAsksAgain(t *testing.T) {
	// Grant with an expiry, act, advance the clock past it, act again, assert a
	// second permission request appears.
	//
	// This is the regression test for the gap where every human approval path
	// dropped expires_at, making every "Allow" permanent.
}
```

The first test's second assertion is the load-bearing one.

- [ ] **Step 2: Run red, implement, verify, commit**

---

### Task 6: Notes as a memory source

*(Consumes K3 — verify the memory repo and capability names before dispatch.)*

**Files:**
- Create: `server/internal/apps/obsidian/index.go`
- Test: `server/internal/apps/obsidian/index_test.go`

**Interfaces:**
- Produces: `obsidian.IndexNotes(ctx, svc *Service, mem repo.MemoryRepo, spaceID string) (int, error)`.

Indexed notes become `memory_entry` rows of kind `pointer`, with `source_kind = application` and `source_ref = <note path>`. The pointer holds a summary and the path, **not the note body**: the vault is the content, memory is the index.

Writing them requires `memory.write` against the space, exactly like an agent write. The application gets no privileged path — that is the point of the slice.

Indexing is triggered explicitly, not on a schedule. A background reindex is a scheduling problem, and scheduling belongs to Routines.

- [ ] **Step 1: Write the failing tests**

```go
func TestIndexWritesPointersNotBodies(t *testing.T) {
	// Index a note with a long body; assert the stored entry's content does not
	// contain the body, and its source_ref is the note path.
}

func TestIndexRequiresMemoryWriteGrant(t *testing.T) {
	// With no memory.write grant for the space, assert indexing is refused and
	// no entry is written. The application is not privileged.
}

func TestStaleePointerIsMarkedInvalidNotDeleted(t *testing.T) {
	// A note deleted between index and access: assert the pointer is marked
	// invalid on the failed access rather than removed, so the contradiction
	// stays visible.
}
```

Fix the typo in the third test's name when you write it out.

- [ ] **Step 2: Run red, implement, verify, commit**

---

### Task 7: Settings, wiring and the exit criteria

*(Consumes K2 and K3 — verify before dispatch.)*

**Files:**
- Create: `server/internal/api/apps/obsidian_routes.go`
- Modify: `server/serverapp/di.go` — register the application at boot
- Create: `src/features/settings/components/ObsidianSettings.vue`
- Test: `server/internal/api/apps/obsidian_routes_test.go`, plus a component test

**Interfaces:**
- Produces: settings for `baseUrl` (url), `apiKey` (**secret**), `vaultRoot` (string), `tlsMode` (enum).

`apiKey` uses the existing secret path: AES-GCM at rest, masked on read, and the mask also means "leave unchanged" on write — so a save that does not touch the field preserves it.

- [ ] **Step 1: Write the failing tests**

Assert the key is never returned in a response body; assert an unchanged masked value does not overwrite the stored secret; assert `tlsMode` rejects a value outside the enum.

- [ ] **Step 2: Run red, implement, verify both gates**

- [ ] **Step 3: Walk the exit criteria by hand and record the result in the commit body**

The MVP is done when this sequence runs, visibly, with no kernel change specific to Obsidian:

1. a Routine fires on its schedule and starts a Task
2. the Task's agent receives a memory push at spawn, and what was pushed is inspectable afterwards
3. the agent reads from the vault; `obsidian.read` is granted at project scope and the call proceeds without asking
4. the agent tries to delete a note; `obsidian.delete` has no grant, so a permission request appears in the dashboard band
5. the human grants it, scoped to this task, with an expiry that is actually recorded
6. the agent deletes the note and writes what it learned into memory, gated by `memory.write`
7. a second attempt after the expiry asks again

**Step 5 is the one that fails today** — every human approval path currently drops `expires_at`. **Step 7 is the proof that it stopped failing.**

If any step does not hold, report it rather than adjusting the criteria.

- [ ] **Step 4: Commit**

---

### Task 8: Documentation and the full gate

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `docs/guides/security.md`, `PRIVACY.md`, `docs/README.md`

The security guide gains the TLS modes and what `insecure-loopback` actually means. `PRIVACY.md` gains the Obsidian settings row and the fact that vault content is read but only pointers are stored.

- [ ] **Step 1: Write the documentation.**
- [ ] **Step 2: Run the full gate** — both Go and frontend — pasting raw output.
- [ ] **Step 3: Verify `git status --short server/internal/db/ent/` is empty and `server/frontend/dist/.gitkeep` still exists.**
- [ ] **Step 4: Commit.**
- [ ] **Step 5: Stop.** No push, no pull request.

---

## Out of scope for this plan

| Item | Where it belongs |
|---|---|
| Scheduled reindex | That is a Routine; wiring it before the gate is proven would hide whether the gate is right |
| Writing memory back into the vault as notes | Needs a format decision and a conflict story; the materializer will have both |
| Attachment and image handling | The slice is about the gate, not Obsidian's full surface |
| A second vault | One vault proves the model; two is a scoping question the registry already answers |
| Mail, calendar, social connectors | MLP and V2 |
| Effort for adapters that do not support it | Reported as inapplicable, never emulated |
