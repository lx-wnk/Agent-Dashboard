# Task Dependency System — Design Spec

> **For agentic workers:** Use `superpowers:writing-plans` to turn this spec into an implementation plan.

**Goal:** Allow tasks to declare predecessor tasks they must wait for before the orchestrator picks them up, enabling sequential pipelines of follow-up tasks.

**Architecture:** Junction table `task_dependencies` (Option A — normalized, FK-constrained), checked at orchestrator pickup time. Dependents are automatically handled when a predecessor reaches a terminal stage.

---

## Data Model

### `task_dependencies` table (new migration)

```sql
CREATE TABLE task_dependencies (
  id               TEXT PRIMARY KEY,
  task_id          TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  depends_on_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  required_stage   TEXT NOT NULL DEFAULT 'done'
                   CHECK (required_stage IN ('done', 'cancelled')),
  on_cancel_action TEXT NOT NULL DEFAULT 'on_hold'
                   CHECK (on_cancel_action IN ('cancel', 'start', 'on_hold')),
  created_at       TEXT NOT NULL,
  UNIQUE(task_id, depends_on_id)
);
```

**Field semantics:**
- `task_id` — the dependent (the task that waits)
- `depends_on_id` — the prerequisite (the task that must finish first)
- `required_stage` — terminal stage the prerequisite must reach (`done` | `cancelled`). Default: `done`.
- `on_cancel_action` — what to do with the dependent when the prerequisite reaches `cancelled`:
  - `cancel` — cascade-cancel the dependent
  - `start` — unblock immediately, let it proceed
  - `on_hold` — move dependent to `on_hold` for human review (default)

`ON DELETE CASCADE` on both FKs ensures orphan-free cleanup when either task is deleted.

### `TaskDependency` TypeScript type (new, `src/types.ts`)

```typescript
export interface TaskDependency {
  id: string
  taskId: string
  dependsOnId: string
  dependsOnTitle: string     // denormalized for display, joined at read time
  dependsOnStage: PipelineStage  // current stage of the prerequisite
  requiredStage: 'done' | 'cancelled'
  onCancelAction: 'cancel' | 'start' | 'on_hold'
  createdAt: string
}
```

### `PipelineTask` additions (`src/types.ts`)

```typescript
dependencies?: TaskDependency[]   // prerequisites of this task
dependents?: TaskDependency[]     // tasks waiting on this task
isBlocked?: boolean               // true if any dependency is unmet
```

---

## Repository — `server/db/taskDependenciesRepo.ts` (new file)

| Function | Description |
|---|---|
| `addDependency(taskId, dependsOnId, requiredStage, onCancelAction)` | Insert dependency. Throws if it would create a cycle (DFS check). |
| `removeDependency(taskId, dependsOnId)` | Remove dependency by both IDs. Returns `boolean`. |
| `getDependenciesFor(taskId)` | All `TaskDependency` rows where `task_id = taskId` (what this task waits for). Joins `tasks` for `dependsOnTitle` / `dependsOnStage`. |
| `getDependentsOf(taskId)` | All `TaskDependency` rows where `depends_on_id = taskId` (who is waiting on this task). |
| `isBlocked(taskId)` | Returns `true` if any prerequisite has not yet reached its `required_stage`. Implemented as a single SQL query. |

**Cycle detection:** `addDependency` runs a DFS from `dependsOnId` following existing `task_id → depends_on_id` edges. If it reaches `taskId`, the insert would create a cycle — throw `Error('Dependency would create a cycle')`.

---

## Orchestrator Integration (`server/pipeline/`)

### `tasksRepo.ts` — `listPickableTasks()`

Add a subquery/join that excludes tasks with at least one unmet dependency:

```sql
WHERE NOT EXISTS (
  SELECT 1 FROM task_dependencies td
  JOIN tasks t2 ON t2.id = td.depends_on_id
  WHERE td.task_id = tasks.id
    AND t2.current_stage != td.required_stage
)
```

This keeps blocked tasks invisible to the pickup loop without touching `pickNextTasksForFreeSlots`.

### `orchestrator.ts` — `applyTransition()`

After a task transitions to `done` or `cancelled`, call `handleDependentTasks(taskId, newStage, broadcast)` (new helper, co-located in `orchestrator.ts`):

1. Load all dependents via `getDependentsOf(taskId)` grouped by their `on_cancel_action`.
2. For each dependent, call `isBlocked(dependent.taskId)` — if still blocked by other predecessors, skip.
3. If the predecessor reached `done`: dependent is now unblocked, no action needed (tick will pick it up).
4. If the predecessor reached `cancelled`:
   - `on_cancel_action === 'cancel'` → `updateTask(dependentId, { currentStage: 'cancelled' })` + broadcast
   - `on_cancel_action === 'start'` → unblock (no action; tick picks it up)
   - `on_cancel_action === 'on_hold'` → `updateTask(dependentId, { currentStage: 'on_hold' })` + broadcast
5. All mutations happen inside the same SQLite transaction as the parent transition.

---

## API Routes (`server/routes/taskRoutes.ts`)

Four new endpoints:

| Method | Path | Description |
|---|---|---|
| `GET` | `/tasks/:id/dependencies` | List all prerequisites for a task |
| `GET` | `/tasks/:id/dependents` | List all tasks waiting on this task |
| `POST` | `/tasks/:id/dependencies` | Add a dependency |
| `DELETE` | `/tasks/:id/dependencies/:depId` | Remove a dependency |

**POST body:**
```typescript
{
  dependsOnId: string          // required
  requiredStage?: 'done' | 'cancelled'     // default 'done'
  onCancelAction?: 'cancel' | 'start' | 'on_hold'  // default 'on_hold'
}
```

**Error responses:**
- `400` — cycle detected
- `404` — task or dependsOnId not found
- `409` — dependency already exists

Existing `GET /tasks/:id` and `GET /tasks` enrich the response with `dependencies`, `dependents`, and `isBlocked` fields (computed via joins in `getTaskById` / `listTasks`).

---

## MCP Tools (`server/mcp/tools/writeTools.ts`)

Two new tools, scope `tasks:write`:

**`add_dependency`**
```typescript
{
  task_id: z.string(),
  depends_on_id: z.string(),
  required_stage: z.enum(['done', 'cancelled']).default('done'),
  on_cancel_action: z.enum(['cancel', 'start', 'on_hold']).default('on_hold'),
}
```
Returns the created `TaskDependency`. Throws MCP error `-32003` on cycle.

**`remove_dependency`**
```typescript
{
  task_id: z.string(),
  depends_on_id: z.string(),
}
```
Returns `{ removed: boolean }`.

Both tools must be added to `TOOL_SCOPE_MAP` in `server/mcp/mcpAuth.ts`.

---

## UI

### TaskCard (`src/components/TaskCard.vue`)

- If `isBlocked === true`: display a lock badge ("🔒 Blocked") in the card header; apply a subtle grey/dim overlay to the card.
- Tooltip on hover: list of unmet prerequisite titles with their required stage.

### TaskModal (`src/components/TaskModal.vue`)

New "Abhängigkeiten" section with two sub-lists:

**Wartet auf (prerequisites):**
- Each row: predecessor title + current stage chip (green if `required_stage` met, grey otherwise) + trash icon to remove
- Inline add-form: task search/slug input + `required_stage` dropdown + `on_cancel_action` dropdown + "Hinzufügen" button

**Wird benötigt von (dependents):**
- Each row: dependent title + stage chip (read-only)

### PipelineBoard / KanbanBoard (`src/components/KanbanBoard.vue`, `PipelineBoard.vue`)

- Blocked tasks appear in their current stage column (`backlog`) but visually dimmed (e.g. `opacity: 0.5`) with a lock icon overlay.
- No column change — blocked tasks stay in `backlog`, they just can't be picked.

---

## Error Handling & Edge Cases

| Scenario | Handling |
|---|---|
| Self-dependency (`task_id === depends_on_id`) | Rejected at repo level (cycle check catches it immediately) |
| Dependency on already-terminal task | Allowed — `isBlocked` immediately returns `false`, task picks up on next tick |
| Deleting a task that others depend on | `ON DELETE CASCADE` removes the dependency rows; dependents are unblocked on next tick |
| Multiple predecessors, one cancelled | `handleDependentTasks` only acts when `isBlocked` returns `false` (all others also done) |
| Circular dependency of 3+ tasks | DFS traversal catches it regardless of chain length |

---

## Testing

- Unit: `taskDependenciesRepo` — add/remove/isBlocked/cycle detection, cascade delete
- Unit: `orchestrator.handleDependentTasks` — all three `on_cancel_action` paths
- Unit: `listPickableTasks` — blocked tasks excluded, unblocked tasks included
- Integration: full lifecycle — create two tasks, add dependency, first completes → second starts
- Integration: cancellation cascade — first cancelled → second moves to `on_hold`
- E2E: add dependency via UI, verify card shows lock badge, complete first task, verify second starts
