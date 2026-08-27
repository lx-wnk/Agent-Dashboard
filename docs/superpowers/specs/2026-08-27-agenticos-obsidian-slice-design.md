# AgenticOS — Slice One: Obsidian, and Effort Tuning

**Date:** 2026-08-27
**Status:** Approved design
**Stage:** MVP (units A1 and S2)
**Parent:** `2026-08-27-agenticos-overview-design.md`
**Implements:** decision D8 (a vertical slice drives the kernel, and it is dangerous on purpose)

---

## 1. Purpose

Prove the kernel against one real Application, end to end: registry entry → capability declaration
→ grants → gated calls → memory writes. And prove it against an **irreversible** action, because a
gate designed against a harmless case is a gate designed against assumptions.

Obsidian is the choice because it is dangerous enough to test the gate (deleting a note is not
undoable) and cheap enough not to drag an OAuth flow into the first slice.

S2 — reasoning effort per task — rides along in the same stage. It is unrelated to the slice, it
is small, and it is used daily. Bundling it costs nothing and delays nothing.

---

## 2. Scope

**In:** an `application`-kind registry resource; a capability declaration; the four gated
operations; note indexing into memory as a source; the settings surface; effort resolution.

**Out:** anything a second application would need but this one does not. No OAuth, no token
refresh, no webhook ingestion, no bidirectional sync, no vault-wide reindex on a schedule. Those
arrive with mail, which is the point of having a second slice.

---

## 3. What exists in the repo today

**Nothing.** A search for `obsidian` across the Go and TypeScript trees returns zero matches. This
is a genuinely new component, unlike almost everything else in the AgenticOS design.

What it can build on:

| Need | Existing mechanism |
|---|---|
| Encrypted settings | `plugin_setting` with AES-GCM ciphertext plus nonce (`schema/plugin_setting.go:11-12`), `secretbox` master key, `MaskedSentinel = "********"` where the sentinel also means "leave unchanged" on write (`plugin/settings_service.go:17,73-75,124-127`) |
| Typed setting fields | `SettingField{Key, Type, Label, Secret, Enum}` (`plugin/types.go:25-31`), types `string \| url \| int \| bool \| enum` |
| Outbound URL safety | `validation.IsBlockedHost`, `IsBlockedIP`, `validation.SafeDialContext` — used by the remotes handler (`api/remotes/handler.go:40-86`) |
| Per-stage engine and model | `spawner.adapter_config`, resolution chain `task → project stage → project → global stage → default` (`services/spawner_resolver.go:39-52`) |

---

## 4. The Application

### 4.1 Shape

An in-server module, per the overview's rule that implementation shape is a property rather than a
definition (overview §6.1). Obsidian's Local REST API runs on the same machine; putting a
subprocess in front of an HTTP client would add a hop and no isolation.

The registry entry is what makes it an Application. Nothing about the gate, the grants or the UI
depends on it being in-process.

### 4.2 Capabilities

| Capability | Class | Reversible | Enforceable by |
|---|---|---|---|
| `obsidian.read` | reach | — | server |
| `obsidian.search` | reach | — | server |
| `obsidian.write` | reach | no (overwrites content) | server |
| `obsidian.delete` | reach | **no** | server |

`obsidian.delete` is the whole reason this slice was chosen. It carries `reversible = false`,
which per the capability-gate spec means it never auto-grants from a preset alone and always
requires an explicit scoped grant.

`obsidian.write` is marked irreversible too, and that is deliberate rather than pedantic: writing
over an existing note destroys its content just as thoroughly as deleting it, and a model that
only guards `delete` would give false comfort.

### 4.3 Settings

| Key | Type | Secret |
|---|---|---|
| `baseUrl` | url | no |
| `apiKey` | string | **yes** |
| `vaultRoot` | string | no |
| `tlsMode` | enum — `verify` \| `pinned` \| `insecure-loopback` | no |

`apiKey` uses the existing secret path: encrypted at rest, masked on read, and the mask means
"unchanged" on write.

### 4.4 The TLS question, stated plainly

Obsidian's Local REST API serves HTTPS with a self-signed certificate. The common workaround is
`curl -k` — disabling verification entirely. An application that silently did that would be
shipping a permanently unverified TLS client into a system whose entire security posture is
"local-first, nothing leaves the machine."

Three modes, defaulting to the safest that works:

- `verify` — normal verification. Works if the user has trusted the certificate.
- `pinned` — pin the certificate fingerprint, captured once on first connect and shown to the user
  for confirmation. **The default.** It gives real protection without requiring the user to install
  a certificate.
- `insecure-loopback` — verification off, permitted **only** when the host resolves to loopback,
  and surfaced in the UI as a warning rather than buried in a config file.

### 4.5 The SSRF guard collides with this, and the fix is not to loosen it

`isSafeRemoteURL` (`api/remotes/handler.go:40-65`) rejects loopback, link-local, CGNAT and private
addresses, and `SafeDialContext` re-validates the resolved IP at connect time to defeat DNS
rebinding. Obsidian's API is *on* loopback. The guard would reject it.

**The guard is not relaxed.** Loosening `IsBlockedIP` globally would weaken every outbound call in
the system to accommodate one application. Instead the Obsidian client uses its own dial policy
that permits exactly one host — the configured one, resolved once, pinned to that address — and
that policy is a property of the application's registry entry, visible and revocable.

This is the same shape as the capability model itself: a narrow, named, inspectable exception
rather than a widened default.

---

## 5. Memory integration

Obsidian is the first memory *source*, which is the second thing this slice proves.

- Indexed notes become `memory_entry` rows of kind `pointer`, carrying `source_kind = application`
  and `source_ref = <note path>`. The pointer holds a summary and the path, not the note body —
  the vault is the content, memory is the index.
- Writing them requires `memory.write` against the space, exactly like an agent write. The
  application gets no privileged path.
- Indexing is triggered explicitly, not on a schedule. A background reindex is a scheduling
  problem, and scheduling belongs to Routines.

---

## 6. S2 — effort per task

### 6.1 Where it lives

In `spawner.adapter_config`, which is already a `map[string]string` with the SQL-default handling
that SQLite needs (`schema/spawner.go:31-33`), resolved through the chain that already exists:

```
task.spawner_id → project stageSpawner.<stage> → project.default_spawner_id
               → global stageSpawner.<stage> → is_default spawner → claude-default
```

**Not** a column on `task`. "Effort" is a Claude concept; `adapter_type` already spans
`claude | ollama | openai | custom | acp` (`api/spawners/validation.go:21`), and a task-level column
would force a universal meaning onto something that is not universal.

### 6.2 Per-adapter semantics

Each adapter declares whether it understands effort and what its values mean. An adapter that does
not gets the same treatment as a provider without a skill format: the setting is visibly
inapplicable, not silently dropped.

### 6.3 UI

Effort sits next to model in the existing per-stage tuning surface, with the resolved value and
its source shown — the resolver already reports which tier won (`SpawnerSource`,
`services/spawner_resolver.go:19-30`), and surfacing that is most of the value.

---

## 7. Exit criteria for the MVP

The slice is done when this sequence runs, visibly, without a kernel change specific to Obsidian:

1. A Routine fires on its schedule and starts a Task.
2. The Task's agent receives a memory push at spawn, and what was pushed is inspectable
   afterwards.
3. The agent reads from the vault. `obsidian.read` is granted at project scope; the call proceeds
   without asking.
4. The agent tries to delete a note. `obsidian.delete` has no grant; a permission request appears
   in the dashboard band.
5. The human grants it, scoped to this task and with an expiry that is actually recorded.
6. The agent deletes the note and writes what it learned into memory, gated by `memory.write`.
7. A second attempt after the expiry passes asks again.

Step 5 is the one that fails today: every human approval path currently drops `expires_at`
(capability-gate spec, G4). Step 7 is the proof that it stopped failing.

---

## 8. Failure modes

| Situation | Behaviour |
|---|---|
| Obsidian not running | The application reports unreachable. Grants stay valid; nothing is revoked because a service blinked |
| API key wrong | 401 surfaced as a configuration error, not as a permission denial. Confusing those two would send the user to the wrong screen |
| Certificate changed under `pinned` | Refuse and surface. A changed certificate on loopback is usually a reinstall, but the user confirms it rather than the system assuming |
| `insecure-loopback` requested for a non-loopback host | Refused at validation, not at connect time |
| Note deleted by the user between index and access | The pointer entry is stale. It is marked invalid on the failed access rather than deleted, so the contradiction is visible |
| Vault path escapes the configured root | Refused. The root is a boundary, and `vaultRoot` is validated as a prefix of every resolved path |
| Grant expired mid-run | Permission request; the task parks (capability-gate spec §6) |

---

## 9. Testing

- **Capability coverage** — one test per capability proving the gate is consulted. The negative
  case matters most: an ungranted `obsidian.delete` must not reach the HTTP client at all.
- **Irreversibility rule** — a preset alone must not satisfy `obsidian.delete`.
- **Expiry round trip** — grant with an expiry, act, advance the clock, act again, assert a second
  permission request. This is the regression test for G4.
- **TLS modes** — `verify` against a trusted cert, `pinned` against a matching and a mismatched
  fingerprint, `insecure-loopback` accepted for `127.0.0.1` and refused for a public host.
- **Dial policy** — the application's client must refuse any host other than the configured one,
  including after a DNS answer changes between resolve and connect.
- **Secret handling** — the API key never appears in a response body, a log line, or an error
  message. Asserted against the store and the wire, not against the UI.
- **Effort resolution** — the same table the spawner resolver already uses, extended with effort,
  including an adapter that does not support it.
- **Path containment** — a note path escaping `vaultRoot` is refused before any request is built.

---

## 10. Deferred

| Item | Why not now |
|---|---|
| Scheduled reindex | That is a Routine, and Routines are already a kind. Wiring it is trivial once the slice works; doing it first would hide whether the gate is right |
| Writing memory back into the vault as notes | Needs a format decision and a conflict story. The materializer will have both by then |
| Attachment and image handling | The slice is about the gate, not about Obsidian's full surface |
| A second vault | One vault proves the model. Two vaults is a scoping question the registry already answers |
