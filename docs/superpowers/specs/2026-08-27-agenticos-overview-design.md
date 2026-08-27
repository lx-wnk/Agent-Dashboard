# AgenticOS — Overview

**Date:** 2026-08-27
**Status:** Approved design, umbrella document
**Source:** `docs/local/agenticOs/initial-ideas.md`
**Siblings:** this document is the frame; each component has its own spec, listed in §12.

---

## 1. What this is

The Agent Dashboard monitors and drives locally running coding agents. It reads what Claude Code
writes to disk, scans processes, runs a multi-stage task pipeline in git worktrees, and exposes an
authenticated MCP control plane.

AgenticOS is the next step: promote the machinery that already exists into an explicit resource
model, close the gaps that keep it a developer tool rather than a system, and extend its reach
from source code into the rest of the working day — notes, mail, calendar, channels.

The organising model is **ARMS**: Applications, Routines, Memory, Skills.

### What "operating system" means here

It is a deliberate analogy, and it only holds in three places:

- **A resource model.** Things the system manages have identity, scope, lifecycle and
  permissions, and they are managed the same way regardless of kind.
- **A single authorization decision.** One place answers "may this happen", even though
  enforcement happens in several.
- **Continuity across sessions.** The system remembers, and what it remembers is structured
  rather than appended to a file.

It does **not** mean process scheduling, resource isolation, a filesystem abstraction, or a
kernel/userland boundary. Where the analogy would push the design toward those, the analogy loses.

---

## 2. Glossary

Precise vocabulary matters here because several of these words already mean something else in
this codebase.

| Term | Meaning in AgenticOS | Not to be confused with |
|---|---|---|
| **Application** | A registry resource that gives the system reach into one external system, or provides infrastructure to it | The frontend "plugin" concept, which is one possible implementation shape |
| **Routine** | A registry resource holding a trigger, a task template, grants and a budget. The only thing besides a human that starts work | `task_schedule`, which is today's narrower cron-only form |
| **Skill** | A unit of know-how owned by the system and materialized into the files agent runtimes read | Claude Code's on-disk skill, which becomes a derived artifact |
| **Memory** | Structured, scoped, expiring knowledge the system holds and delivers to agents | `MEMORY.md`, `CLAUDE.md`, and the `.agent-context/memory/` files, which are today's unstructured stand-ins |
| **Task** | A unit of work with stages, running in a worktree. Unchanged | — |
| **Agent** | A process executing a stage of a task. Unchanged | — |
| **Spawner** | The engine configuration a task runs on: command, args, env, adapter type, model | The provider concept, which describes how sessions are discovered and parsed |
| **Node** | One machine that can execute work and hold config directories | An instance; one node may host more than one dashboard instance |
| **Capability** | A named permission a resource may need, e.g. `mail.send` | A tool name; capabilities are coarser and outlive individual tools |
| **Grant** | A capability bound to a context, optionally with expiry and a limit | A `permission_preset` row, which is today's narrower form |
| **Materialization** | Producing files on a node from owned resources | `drift_alert`, which means model-quality drift and is unrelated |

---

## 3. Where the project stands today

The honest finding from designing this: **most of a kernel already exists, under other names.**
Every row below was read first-hand while writing this document.

| Capability the OS needs | Exists today as | Evidence |
|---|---|---|
| Context-scoped permissions | `permission_preset` — `(user_id, project_cwd, tool, pattern)` | `server/internal/db/ent/schema/permission_preset.go:13-20` |
| Per-task permission overrides | `task_permission` with `manual_override` | `server/internal/db/ent/schema/task_permission.go` |
| Ask-the-human round trip | `permission_request`, surfaced over SSE and the attention band | `server/internal/db/ent/schema/permission_request.go`, `src/utils/attention.ts:38-39` |
| Scope model with implication | `ToolScopeMap`, `ResolveScopes` | `server/internal/mcp/auth.go:17-61` |
| Tool-level enforcement | Bash pattern allow-list and write-tool gate | `server/internal/permissions/allowlist.go` |
| Engine selection with a resolution chain | `spawner` entity; task → project stage → project → global stage → default | `server/internal/db/ent/schema/spawner.go:16-45`, `server/internal/services/spawner_resolver.go:39-52` |
| Free-form engine configuration | `spawner.adapter_config`, a JSON map | `server/internal/db/ent/schema/spawner.go:31-33` |
| Deterministic triggers | Scheduler with stored cron expressions, offline-safe | `server/internal/scheduler/scheduler.go:36-45` |
| Natural-language trigger authoring | `nlcron.go`, `nlcron_llm.go` | `server/internal/scheduler/` |
| Leases for coordination | `coord_lock` with lazy expiry and SQLite busy-retry | `server/internal/db/ent/schema/coord_lock.go` |
| Remote instance registration | `remote_registration` — url plus hashed bearer key | `server/internal/db/ent/schema/remote_registration.go:14-23` |
| Per-provider config directories | `ConfigDirSpec` with env override; `Registry.ConfigDirs()` | `server/internal/provider/descriptor.go:34-42`, `server/internal/provider/registry.go:179-193` |
| Skill and command enumeration | `cmdscope` — user, project and plugin sources, with an editability rule | `server/internal/cmdscope/enumerate.go:209-254` |
| Authorized config file editing | `GET/PUT /api/config/file`, mtime check, atomic temp-file write | `server/internal/api/config/file.go:37-45,192` |
| Subprocess extensions with a proxy | `plugin`, `pluginmgmt`, `plugin-sdk/plugin.schema.json` | `server/internal/plugin/`, `server/internal/pluginmgmt/` |
| Full-text search infrastructure | SQLite FTS5 | `server/internal/db/` |

**Genuine gaps:** there is no memory store of any kind, no path that writes owned data into
`~/.claude`, no capability vocabulary above tool names, and no reasoning-effort control anywhere.
Web push looks wired but is not — `webpush.Service.SendToAll` (`server/internal/webpush/service.go:89`)
has zero production callers, so a permission request raised while no browser tab is open reaches
nobody.

**One live defect surfaced while writing these specs**, recorded here because it changes behaviour
rather than design: `ApproveAllPending` writes `outcome = "approved"`
(`server/internal/api/tasks/permission_service.go:64`), while the ACP gate treats anything other
than `"granted"` as denied (`server/internal/pipeline/stage_handlers.go:342-348`). Approving all
pending permissions therefore *denies* them for an ACP-backed agent. The fix ships first within K2
(capability-gate spec, G6).

This inventory is why the kernel in §5 is smaller than the ambition suggests. The work is mostly
naming and pulling together, not building — which is cheaper, and more dangerous, because pulling
together means operating on five working mechanisms at once.

---

## 4. Decisions

Each decision below was taken explicitly, with the alternatives that lost recorded so a future
reader does not silently re-open them.

### D1 — ARMS is a real resource model, not a naming layer

Applications, Routines, Skills and Memory become first-class resources with a shared registry,
lifecycle and permission anchor.

*Rejected:* a shell that reorganises navigation over today's architecture. It would have shipped
faster and left every new connector a special case, because there would be no shared contract for
identity, scoping or grants.

### D2 — Local single instance first, personal cloud second, teams much later

The MVP runs on one machine. MLP and V2 make the system reachable from a second machine and from
a phone, with remote nodes executing work. Team support — shared spaces, roles, visibility per
resource — is explicitly deferred.

*Consequence, and it is load-bearing:* every resource carries `node_id` from the first migration,
populated with `local`. Retrofitting a node dimension later means a migration through every
resource table and every query that scopes one.

*Rejected:* deferring the node column until it is needed. Cheap now, expensive exactly once.

### D3 — Application means reach; Routine means initiative

An Application gives the system access to something outside itself. It never acts on its own.
A Routine holds a trigger and starts a Task. A Task never starts a Routine.

This yields exactly one execution concept. The alternative — applications as long-lived agents
with their own agenda and budget — is the stronger OS analogy and was rejected because it
multiplies the hard problems: per-application cost control, idle behaviour, and conflict
resolution when two applications act on the same resource at the same time.

### D4 — Memory is owned by the system; Obsidian is an Application

The system holds a structured store: entry, scope, source, validity, confidence. Obsidian is
reach — the system reads from the vault, writes into it, and may index notes as a source, but
does not depend on it for its own memory.

*Rejected:* the vault as single source of truth. It is human-readable and git-versioned, which is
genuinely attractive, but it cannot express scope, expiry, provenance or contradiction, and a
vault that is not running would make the system amnesiac.

### D5 — The database owns Skills; a materializer writes files per node

Claude Code reads skills from disk, so files must exist. They become a derived artifact produced
from owned resources.

*Rejected:* files as source of truth with the UI as an editor. It is the smaller change and
matches today's `api/config/file.go` exactly — but a skill authored on a phone would never reach
a second machine, which contradicts D2.

### D6 — One policy model, several enforcement adapters

A single decision function answers whether an action is permitted. Enforcement happens at three
different points with genuinely different strength, so the model records per capability where it
is actually enforceable rather than implying a guarantee it cannot keep. Detail in §7.

### D7 — Memory reaches agents as a budgeted push at spawn, plus a pull tool for depth

A curated extract under a hard token budget is injected when an agent starts; a search tool
serves anything deeper.

*Rejected:* pull only. It depends on the agent choosing to ask, and it goes silent when the MCP
transport is unavailable — a failure mode observed in this very project, where a configured MCP
server was refusing connections while the work was being done. The agent does not notice the
absence; it simply proceeds uninformed.

*Rejected:* push only, by materializing into `CLAUDE.md`. Every session then pays the full token
cost whether or not the knowledge is relevant, and the file grows monotonically.

### D8 — A vertical slice drives the kernel, and slice one is dangerous on purpose

The kernel is built only as far as the first real application needs it. That application is
Obsidian **including write and delete**.

*Rejected:* Obsidian read-only as the first slice. It would exercise the registry and memory but
not the gate — and a gate designed against a harmless case repeats the mistake that ruled out
kernel-first: designing against assumptions and discovering the flaws once they are already
embedded everywhere.

*Rejected:* mail as the first slice. Highest real-world value and the hardest possible test, but
it would put an OAuth flow, a provider API and delivery semantics into the same slice as an
unproven kernel. When something failed, the cause would be ambiguous.

---

## 4b. Engineering principles

These govern how the design is built, and they are the reason §3's "historically grown" findings
are treated as defects rather than quirks. Renaming and re-scoping existing constructs is
explicitly permitted.

| Principle | Applied here |
|---|---|
| **DRY** | A rule, regex, constant or path template exists once. A second copy is a defect even when both are currently correct |
| **KISS** | The smallest construct that holds. No generic repository, no abstraction over the ORM, no framework for four resource kinds |
| **Clean Code** | Named input structs over positional parameters; no boolean flag that changes what a function means |
| **Spec-driven development** | The spec lands before the code and the code is checked against it. These seven documents are that contract |
| **Single source of truth** | Extends the project's existing canonical-locations table rather than competing with it |
| **OOP** | In Go: small interfaces at the seams, one type owning one responsibility. Not inheritance hierarchies |

The concrete consolidation work these imply is collected in the conventions spec, and each item is
carried by the unit that already touches that code — not as a separate refactoring project.

## 5. The layer model

```
Shell        OS shell (reach apps)   Tuning (model + effort)   Settings (infra apps)
                      │                        │                        │
ARMS         Applications        Routines        Skills        Memory
                      │                        │                        │
Kernel       Resource Registry       Capability Gate       Materializer
                      │                        │                        │
Execution    Agent / Task            Spawner               Refinement
                      │                        │
Storage      SQLite / ent (truth)    Config dirs (derived)
```

Reading rules for this diagram:

- A layer may depend on the layer below it, never above.
- The kernel knows nothing about what a kind *does*. It holds identity, decides permission, and
  produces files. Kind-specific behaviour lives in the ARMS layer.
- The execution layer sits **below** ARMS, not beside it. An agent is not a citizen of the OS; it
  is what happens when a Routine or a human starts a Task. This follows directly from D3.
- Storage has two halves with an explicit direction: the database is truth, config directories
  are produced from it. This inverts today's relationship and is the single most consequential
  change in the whole design.

---

## 6. ARMS semantics

### 6.1 Applications — two classes, one mechanism

| Class | Examples | Surfaces in | Capability style |
|---|---|---|---|
| `reach` | Obsidian, Mail, Calendar, LinkedIn, Instagram | OS shell | Consequential: `mail.send`, `obsidian.delete` |
| `infra` | Auth providers, LLM adapters, voice input | Settings | Structural: affects how the system runs, not what it touches |

Both are registry entries with capability declarations; they differ in where they appear and what
kind of permission question they raise. Without the split, "Application" would cover both a mail
connector and an LLM adapter and would stop carrying meaning.

**Implementation shape is a property, not a definition.** Third-party applications use the
existing subprocess-and-proxy plugin mechanism. In-tree applications may be in-server modules
where a subprocess would add a hop without adding isolation.

### 6.2 Routines — initiative with a budget

A Routine holds:

- a **trigger** — a cron expression in the MVP; event triggers later
- a **task template** — what gets created when it fires
- **grants** — what the work it starts is allowed to do
- a **budget** — a ceiling on tokens or cost per firing, and an overlap policy

Today's `task_schedule` is the trigger and template halves of this. Grants and budget are new.

### 6.3 Skills — owned, versioned, materialized

A skill is content plus metadata: version, scope, origin, test status. Materialization turns it
into the file layout a given runtime expects. Skills inherited from GitHub keep their origin
recorded so an update from upstream does not silently overwrite local edits.

### 6.4 Memory — the only kind that is data rather than an artefact

The three other kinds are things you manage: dozens of them, each with a page. Memory is what
accumulates: tens of thousands of rows, none individually interesting.

The registry therefore holds **memory spaces**, not memory entries. A space is a named, scoped
namespace — one per project, one global, optionally one per application — and it is what grants
attach to. `memory.write` is granted against a space.

This distinction is why ARMS is a good mnemonic and would have been a bad schema principle if
taken literally.

---

## 7. Authorization, honestly

There is one policy model and three enforcement points, and they are not equally strong.

| # | Interception point | Covers | Strength |
|---|---|---|---|
| 1 | Server-internal, at the application call | Every action routed through an Application | Complete |
| 2 | `permissions/allowlist.go` | Agents the dashboard spawned | Complete for those agents |
| 3 | `PreToolUse` hook | Sessions started by hand in a terminal | **Fails open on timeout** |

Consequently every capability carries an **enforceability** attribute naming which adapters can
enforce it, and the UI states this rather than implying uniform protection. A capability that is
only enforceable for orchestrated agents must say so where it is granted.

This is the part of the design most likely to be quietly forgotten during implementation, which
is why it is a numbered decision (D6) and not a footnote.

---

## 8. Non-goals

- **Process isolation between applications.** Applications run in the trust domain of the machine.
  The subprocess plugin boundary is a robustness measure, not a security boundary.
- **A marketplace.** Publishing the resource contract freezes it. Not before the contract has
  been stable through several applications.
- **Team collaboration.** Deferred entirely. No half-measures such as an unused `owner` column.
- **Replacing the pipeline.** Tasks, stages, worktrees and checkpoints stay as they are.
- **Supporting every provider's skill format.** Where a runtime has no equivalent concept,
  materialization is a visible no-op, not an emulation.

---

## 9. Roadmap

### MVP — one resource model, proven on a dangerous slice

| Unit | Content |
|---|---|
| K1 | Resource registry, narrow |
| K2 | Capability gate: vocabulary, expiry, limits, over existing tables; adapters 1 and 2 |
| K3 | Memory store, push and pull delivery |
| A1 | Obsidian as a `reach` application including write and delete |
| S2 | Effort in `adapter_config`, resolved through the spawner chain |

**Exit criteria:** a routine can fire, start a task, receive a memory push, act on the vault
through granted capabilities, hit a limit, produce a permission request, and write back what it
learned — with every step visible in the UI.

Everything in this stage is additive: new tables, new endpoints, a new store. Nothing that works
today is rewired. A failed MVP breaks nothing.

### MLP — useful, and it starts building itself

- A2 Mail, read and send — the second slice, and the first with an OAuth flow
- L1/L2 backlog input, refinement to ready, auto-pick with budget limits
- K4 materializer, and L3 skill authoring on top of it
- S1 OS shell
- Migration of `plugin` and `task_schedule` under the registry

**Entry criterion:** two applications have run through the kernel without kernel changes specific
to either.

### V2 — personal cloud

- C1 node registry, extending `remote_registration`, and safe exposure beyond loopback
- C2 materialization across nodes
- C3 phone-capable surface
- A3 Calendar, A4 social connectors

### Later

Team scoping. Marketplace, if ever.

---

## 10. Cross-cutting concerns

**Multi-node readiness.** `node_id` everywhere from the first migration. Materialization requires
a node lease so two instances on one machine cannot fight over the same files; reachability is not
ownership, a lesson this project already learned once when a desktop instance adopted a foreign
server because a health check returned 200.

**Security posture.** Unchanged: loopback binding, hashed tokens, no outbound calls unless opted
in. New surface comes from applications, and each one is gated per capability. Secrets belonging
to applications use the existing encrypted storage path rather than a new one.

**Documentation.** Per project rules, user-facing changes update `README.md`, `CHANGELOG.md` and
`CONTRIBUTING.md` in the same change. A new resource kind is user-facing.

**Naming.** Do not reuse "drift": `drift_alert` already means model-quality drift per
`(spawner, model, stage, metric_key)`. The full vocabulary agreement, including the rename of the
config explorer's misleading "memory" endpoint to *context files*, is in the conventions spec.

---

## 11. Risks

| Risk | Why it matters | Mitigation |
|---|---|---|
| Two truths during migration | Every feature costs twice, and defects live in the seam | Coexistence limited to two slices; then `plugin` and `task_schedule` migrate |
| Pulling five working mechanisms together | Presets, leases, provider dirs, spawner chain and scope map all work today | The registry references them rather than absorbing them; one migrates at a time |
| Memory scoring is wrong at first | Bad injections waste tokens and actively mislead agents | Every injection is recorded, so the heuristic is measurable from day one |
| The materializer writes into user files | The only component with real destructive potential | Deferred to MLP; lease-gated; reports conflicts instead of overwriting |
| "One gate" overpromises | A capability may appear enforced when it is not | Per-capability enforceability, surfaced where it is granted |
| Scope creep into a life-management product | Mail, calendar and social are each large | One slice at a time, each gated on the previous one exercising the kernel unchanged |

---

## 12. Component specs

| Spec | Covers | Stage |
|---|---|---|
| `2026-08-27-agenticos-resource-registry-design.md` | K1 — identity, scope, node, lifecycle | MVP |
| `2026-08-27-agenticos-capability-gate-design.md` | K2 — capabilities, grants, enforcement adapters | MVP |
| `2026-08-27-agenticos-memory-design.md` | K3 — store, retrieval, push and pull delivery | MVP |
| `2026-08-27-agenticos-obsidian-slice-design.md` | A1 and S2 — the first vertical slice | MVP |
| `2026-08-27-agenticos-materializer-design.md` | K4 — targets, leases, format adapters, conflicts | MLP |
| `2026-08-27-agenticos-conventions-design.md` | Vocabulary, duplications to collapse, traps, standards | carried by each unit |

---

## Appendix — review findings folded into this design

These came from a review of the first draft of this design, not from implementation.

| # | Finding | Resolution |
|---|---|---|
| R1 | "One gate" is not enforceable at one point | One model, three adapters, per-capability enforceability (§7) |
| R2 | A polymorphic registry table degenerates into nullable columns | Narrow registry plus per-kind tables (registry spec) |
| R3 | Memory delivery was missing from the design entirely | Budgeted push plus pull tool (D7, memory spec) |
| R4 | The materializer target is a cross product, not a folder | Node × config dir × provider (materializer spec) |
| R5 | "Drift" already means something else in this codebase | Renamed concept (§10) |
| R6 | The gate is an extension, not a rebuild | Built on `permission_preset` and `task_permission` (§3) |
| R7 | "Application" loses meaning if everything is one | `reach` versus `infra` (§6.1) |
| R8 | The first slice did not exercise the gate | Slice one includes write and delete (D8) |
| R9 | Multiple instances would fight over the same files | Node lease (§10) |
| R10 | Effort is not a universal concept across providers | Stored in `adapter_config`, resolved through the spawner chain (§9) |
