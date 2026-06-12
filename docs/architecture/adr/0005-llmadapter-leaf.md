# ADR-0005: Extract `llmadapter` Leaf Package

**Status:** Accepted
**Date:** 2026-06-09

## Context

The LLM-adapter code — the abstraction that lets a stage agent be spawned
through Ollama, OpenAI, or an arbitrary custom CLI instead of the native
`claude` subprocess (see [ADR-0003](0003-pluggable-spawners.md)) — lived
inside `server/internal/pipeline/`:

- `llm_spawner.go` — `LLMSpawner` / `StreamingLLMSpawner` interfaces, `LLMSpawnArgs`
- `llm_custom.go`, `llm_ollama.go`, `llm_openai.go` — concrete adapters
- `adapter_factory.go` — `NewLLMSpawnerFromSpawner(*ent.Spawner)` dispatch
- `adapter_catalog.go` — `AvailableAdapters` catalog

None of these files reference the pipeline state machine (orchestrator,
stage handlers, completion detector, sweeps). Their only intra-project
dependency is `db/ent` (the `Spawner` row type). They are, in effect, a
self-contained leaf that happened to be parked in a high-level package.

Because they lived in `pipeline/`, two consumers were forced to import the
entire orchestrator package just to reach adapter symbols:

- `refine/spawner.go` imported `pipeline` for `NewLLMSpawnerFromSpawner`,
  `StreamingLLMSpawner`, and `LLMSpawnArgs`.
- `api/adapters/handler.go` imported `pipeline` for `AvailableAdapters`.

These were **upward-looking runtime edges** (`refine -> pipeline`,
`api/adapters -> pipeline`) that the layering rules in `task-pipeline.md`
do not sanction — neither symbol appears in the routes/mcp runtime-import
whitelist, and `refine/` has no business depending on the state machine at
all. They existed only as an accident of file placement (finding Arch-P2-1).

## Decision

Move the six production files plus their six test files into a new leaf
package `server/internal/llmadapter/` (`package llmadapter`; internal
tests stay `package llmadapter`, the three external streaming tests become
`package llmadapter_test`).

The leaf depends only on the standard library and `db/ent`. Its public
surface is unchanged: `LLMSpawner`, `StreamingLLMSpawner`, `LLMSpawnArgs`,
`NewLLMSpawnerFromSpawner`, `AvailableAdapters`, and the concrete adapter
types.

Rewire the three consumers to import `llmadapter`:

| Consumer | Symbols |
|---|---|
| `pipeline/stage_handlers.go` | `NewLLMSpawnerFromSpawner`, `LLMSpawnArgs` |
| `refine/spawner.go` | `NewLLMSpawnerFromSpawner`, `StreamingLLMSpawner`, `LLMSpawnArgs` |
| `api/adapters/handler.go` | `AvailableAdapters` |

No logic, signature, schema, or behaviour changes — this is a pure move +
re-import.

## Consequences

**Removed edges.** The runtime edges `refine -> pipeline` and
`api/adapters -> pipeline` are deleted. Both packages now depend downward
on the `llmadapter` leaf instead.

**`pipeline -> llmadapter` is legal.** `stage_handlers.go` still needs the
adapter factory; it now reaches *down* to a leaf rather than referencing a
sibling file in its own package. This is a normal high-to-low dependency.

**`NewLLMSpawnerFromSpawner` is no longer a cross-pipeline reach.** Prior
to this change, `api/adapters` and `refine` touching adapter symbols looked
like (undocumented) reaches into pipeline internals. After the move there is
nothing pipeline-specific about the call — it targets a leaf package whose
only dependency is `db/ent`.

**Import graph.** Updated in `.agent-context/architecture.md` (Go layer
direction) and `.agent-context/task-pipeline.md` (Go import graph). The
`llmadapter` node is documented as a leaf importable by `pipeline/`,
`refine/`, and `api/adapters/`.

## Alternatives Considered

1. **Leave it in `pipeline/`, whitelist the symbols.** Adding
   `NewLLMSpawnerFromSpawner` / `AvailableAdapters` to the routes/mcp
   runtime-import whitelist would legalise the edges on paper but keep the
   adapter code coupled to the orchestrator package — every `refine` or
   `api/adapters` build would still pull in the state machine. Rejected:
   treats the symptom, not the misplacement.

2. **Move into `services/`.** `services/` is the existing home for
   stateless helpers. But adapters are not orchestration helpers; they are
   a cohesive domain (spawner transport) with their own catalog and
   factory, and `services/` may import `pipeline` types, which would blur
   the leaf guarantee. A dedicated leaf keeps the dependency floor at
   `db/ent`.

3. **Merge adapters into `db/ent`-adjacent code.** Rejected: adapters carry
   runtime behaviour (HTTP calls, subprocess exec) that does not belong in
   the data layer.
