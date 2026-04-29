# Task Dependency System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow tasks to declare prerequisites so the orchestrator only picks them up once all required predecessors reach a terminal stage (`done` or `cancelled`).

**Architecture:** Junction table `task_dependencies` holds predecessor links. `listPickableTasks` excludes blocked tasks via a NOT EXISTS subquery. `handleDependentTasks` (private orchestrator method) is called after every terminal transition (`done` from `applyTransition`; `cancelled` from the cancel route via `notifyTaskTerminated`). UI shows a lock badge on blocked cards and a full dependency panel in the modal.

**Tech Stack:** better-sqlite3 (SQLite), Express, Vue 3 Composition API, Vitest, Zod

---

## File Structure

**New files:**
- `server/db/taskDependenciesRepo.ts` — CRUD + cycle detection for `task_dependencies`
- `server/mcp/tools/dependencyTools.ts` — two MCP tools: `add_dependency`, `remove_dependency`

**Modified files:**
- `server/db/schema.sql` — add `task_dependencies` table
- `server/db/client.ts` — add runtime `CREATE TABLE IF NOT EXISTS` migration for existing DBs
- `server/db/rowMappers.ts` — add `TaskDependencyRow`, `rowToTaskDependency`, extend `TaskRow` with `is_blocked?`
- `src/types.ts` — add `TaskDependency` interface; add `isBlocked?` to `PipelineTask`
- `server/db/tasksRepo.ts` — extend `getTaskById`/`listTasks` queries with `is_blocked` subquery; update `listPickableTasks` to exclude blocked tasks
- `server/pipeline/orchestrator.ts` — add private `handleDependentTasks`, call from `applyTransition` `done` case; add public `notifyTaskTerminated`
- `server/routes/taskRoutes.ts` — 4 new dependency REST endpoints; call `notifyTaskTerminated` in cancel route
- `server/mcp/mcpAuth.ts` — add `add_dependency` and `remove_dependency` to `TOOL_SCOPE_MAP`
- `server/mcp/mcpServer.ts` — register `dependencyTools`
- `src/components/TaskCard.vue` — add blocked badge + dim styling
- `src/components/TaskModal.vue` — add dependencies section (prerequisites + dependents + add/remove form)
- `src/composables/useTasks.ts` — add `fetchDependencies`, `fetchDependents`, `addDependency`, `removeDependency` API calls
- `server/db/db.test.ts` — add `taskDependenciesRepo` describe block

---

### Task 1: DB schema + migration for `task_dependencies`

**Files:**
- Modify: `server/db/schema.sql`
- Modify: `server/db/client.ts`

- [ ] **Step 1: Add `task_dependencies` table to `server/db/schema.sql`**

Append after the `api_keys` block (after line 179):

```sql
-- Task dependency links: task_id must wait for depends_on_id to reach required_stage.
-- ON DELETE CASCADE: removing either task automatically cleans up its dependency rows.
CREATE TABLE IF NOT EXISTS task_dependencies (
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

CREATE INDEX IF NOT EXISTS idx_task_dependencies_task ON task_dependencies(task_id);
CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_id);
```

- [ ] **Step 2: Add runtime migration in `server/db/client.ts`**

In `runMigrations()`, append after the existing runtime ALTER TABLE block (after line 51 — after the `CREATE INDEX IF NOT EXISTS idx_tasks_picker` line):

```typescript
  // Runtime migration: create task_dependencies for DBs created before this feature.
  // schema.sql uses CREATE TABLE IF NOT EXISTS which is idempotent for new DBs.
  connection.exec(`
    CREATE TABLE IF NOT EXISTS task_dependencies (
      id               TEXT PRIMARY KEY,
      task_id          TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
      depends_on_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
      required_stage   TEXT NOT NULL DEFAULT 'done',
      on_cancel_action TEXT NOT NULL DEFAULT 'on_hold',
      created_at       TEXT NOT NULL,
      UNIQUE(task_id, depends_on_id)
    )
  `)
  connection.exec(`CREATE INDEX IF NOT EXISTS idx_task_dependencies_task ON task_dependencies(task_id)`)
  connection.exec(`CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_id)`)
```

- [ ] **Step 3: Run tests to verify schema change doesn't break existing DB suite**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: All existing `describe` blocks pass; no new failures.

- [ ] **Step 4: Commit**

```bash
git add server/db/schema.sql server/db/client.ts
git commit -m "feat: add task_dependencies schema and runtime migration"
```

---

### Task 2: Types + row mappers

**Files:**
- Modify: `src/types.ts`
- Modify: `server/db/rowMappers.ts`

- [ ] **Step 1: Add `TaskDependency` interface and extend `PipelineTask` in `src/types.ts`**

After the `TaskPriority` type (after line 104), add:

```typescript
export interface TaskDependency {
  id: string
  taskId: string
  dependsOnId: string
  dependsOnTitle: string
  dependsOnStage: PipelineStage
  requiredStage: 'done' | 'cancelled'
  onCancelAction: 'cancel' | 'start' | 'on_hold'
  createdAt: string
}
```

In the `PipelineTask` interface, add two optional fields after the existing optional computed fields (`needsUser?`, `latestStageRunStatus?`, etc.):

```typescript
  isBlocked?: boolean
```

- [ ] **Step 2: Add `TaskDependencyRow` + `rowToTaskDependency` in `server/db/rowMappers.ts`**

After the `ApiKeyRow` interface (after line 202), add:

```typescript
export interface TaskDependencyRow {
  id: string
  task_id: string
  depends_on_id: string
  depends_on_title: string  // joined from tasks
  depends_on_stage: string  // joined from tasks
  required_stage: string
  on_cancel_action: string
  created_at: string
}

export function rowToTaskDependency(row: TaskDependencyRow): TaskDependency {
  return {
    id: row.id,
    taskId: row.task_id,
    dependsOnId: row.depends_on_id,
    dependsOnTitle: row.depends_on_title,
    dependsOnStage: row.depends_on_stage as PipelineStage,
    requiredStage: row.required_stage as 'done' | 'cancelled',
    onCancelAction: row.on_cancel_action as 'cancel' | 'start' | 'on_hold',
    createdAt: row.created_at,
  }
}
```

Also add `TaskDependency` to the import at the top of `rowMappers.ts`:

```typescript
import type {
  ApiKey,
  AuditEntry,
  McpScope,
  NotificationPreference,
  PermissionRequest,
  PipelineStage,
  PipelineTask,
  StageRun,
  StageRunStatus,
  TaskDependency,
  TaskPermission,
  TaskPriority,
} from '../../src/types.js'
```

Extend `TaskRow` with the optional `is_blocked` field (add after line 36 `priority: string`):

```typescript
  is_blocked?: number  // 0 | 1 computed subquery, present in enriched queries
```

In `rowToTask`, add `isBlocked` mapping after `priority`:

```typescript
    isBlocked: row.is_blocked === 1,
```

- [ ] **Step 3: Run typecheck**

```bash
pnpm typecheck
```

Expected: No errors.

- [ ] **Step 4: Commit**

```bash
git add src/types.ts server/db/rowMappers.ts
git commit -m "feat: add TaskDependency type and isBlocked to PipelineTask"
```

---

### Task 3: `taskDependenciesRepo.ts` — CRUD + cycle detection

**Files:**
- Create: `server/db/taskDependenciesRepo.ts`
- Modify: `server/db/db.test.ts`

- [ ] **Step 1: Write the failing tests in `server/db/db.test.ts`**

Add a new `describe('taskDependenciesRepo', ...)` block at the end of the file (after the `token helpers` describe block):

```typescript
import {
  addDependency,
  getDependenciesFor,
  getDependentsOf,
  isBlocked,
  removeDependency,
  removeDependencyById,
} from './taskDependenciesRepo.js'

describe('taskDependenciesRepo', () => {
  it('addDependency creates a row retrievable by getDependenciesFor', () => {
    const a = createTask({ slug: 'dep-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'dep-b', title: 'B', cwd: '/b' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    const deps = getDependenciesFor(b.id)
    expect(deps).toHaveLength(1)
    expect(deps[0].dependsOnId).toBe(a.id)
    expect(deps[0].requiredStage).toBe('done')
    expect(deps[0].onCancelAction).toBe('on_hold')
  })

  it('getDependentsOf returns tasks waiting on a given task', () => {
    const a = createTask({ slug: 'dep-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'dep-d', title: 'D', cwd: '/d' })
    addDependency(b.id, a.id, 'done', 'cancel')
    const dependents = getDependentsOf(a.id)
    expect(dependents).toHaveLength(1)
    expect(dependents[0].taskId).toBe(b.id)
  })

  it('isBlocked returns true when prerequisite is not at required stage', () => {
    const a = createTask({ slug: 'dep-e', title: 'E', cwd: '/e' })
    const b = createTask({ slug: 'dep-f', title: 'F', cwd: '/f' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    expect(isBlocked(b.id)).toBe(true)
  })

  it('isBlocked returns false when prerequisite has reached required stage', () => {
    const a = createTask({ slug: 'dep-g', title: 'G', cwd: '/g' })
    const b = createTask({ slug: 'dep-h', title: 'H', cwd: '/h' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    updateTask(a.id, { currentStage: 'done' })
    expect(isBlocked(b.id)).toBe(false)
  })

  it('isBlocked returns false when task has no dependencies', () => {
    const a = createTask({ slug: 'dep-i', title: 'I', cwd: '/i' })
    expect(isBlocked(a.id)).toBe(false)
  })

  it('isBlocked true when at least one of two deps is unmet', () => {
    const a = createTask({ slug: 'dep-j', title: 'J', cwd: '/j' })
    const b = createTask({ slug: 'dep-k', title: 'K', cwd: '/k' })
    const c = createTask({ slug: 'dep-l', title: 'L', cwd: '/l' })
    addDependency(c.id, a.id, 'done', 'on_hold')
    addDependency(c.id, b.id, 'done', 'on_hold')
    updateTask(a.id, { currentStage: 'done' }) // only one done
    expect(isBlocked(c.id)).toBe(true)
  })

  it('removeDependency removes the row', () => {
    const a = createTask({ slug: 'dep-m', title: 'M', cwd: '/m' })
    const b = createTask({ slug: 'dep-n', title: 'N', cwd: '/n' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    expect(getDependenciesFor(b.id)).toHaveLength(1)
    const removed = removeDependency(b.id, a.id)
    expect(removed).toBe(true)
    expect(getDependenciesFor(b.id)).toHaveLength(0)
  })

  it('addDependency rejects self-dependency', () => {
    const a = createTask({ slug: 'dep-o', title: 'O', cwd: '/o' })
    expect(() => addDependency(a.id, a.id, 'done', 'on_hold')).toThrow('cycle')
  })

  it('addDependency rejects direct cycle A→B then B→A', () => {
    const a = createTask({ slug: 'dep-p', title: 'P', cwd: '/p' })
    const b = createTask({ slug: 'dep-q', title: 'Q', cwd: '/q' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    expect(() => addDependency(a.id, b.id, 'done', 'on_hold')).toThrow('cycle')
  })

  it('addDependency rejects 3-node cycle A→B→C then C→A', () => {
    const a = createTask({ slug: 'dep-r', title: 'R', cwd: '/r' })
    const b = createTask({ slug: 'dep-s', title: 'S', cwd: '/s' })
    const c = createTask({ slug: 'dep-t', title: 'T', cwd: '/t' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    addDependency(c.id, b.id, 'done', 'on_hold')
    expect(() => addDependency(a.id, c.id, 'done', 'on_hold')).toThrow('cycle')
  })

  it('CASCADE: deleting a prerequisite removes its dependency rows', () => {
    const a = createTask({ slug: 'dep-u', title: 'U', cwd: '/u' })
    const b = createTask({ slug: 'dep-v', title: 'V', cwd: '/v' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    deleteTask(a.id)
    expect(getDependenciesFor(b.id)).toHaveLength(0)
    expect(isBlocked(b.id)).toBe(false)
  })

  it('addDependency uses defaults: required_stage=done, on_cancel_action=on_hold', () => {
    const a = createTask({ slug: 'dep-w', title: 'W', cwd: '/w' })
    const b = createTask({ slug: 'dep-x', title: 'X', cwd: '/x' })
    addDependency(b.id, a.id)
    const deps = getDependenciesFor(b.id)
    expect(deps[0].requiredStage).toBe('done')
    expect(deps[0].onCancelAction).toBe('on_hold')
  })
})
```

- [ ] **Step 2: Run failing tests**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: All `taskDependenciesRepo` tests FAIL with "Cannot find module './taskDependenciesRepo.js'".

- [ ] **Step 3: Create `server/db/taskDependenciesRepo.ts`**

```typescript
import type { Database } from 'better-sqlite3'
import type { TaskDependency } from '../../src/types.js'
import type { TaskDependencyRow } from './rowMappers.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { rowToTaskDependency } from './rowMappers.js'

function nowIso(): string {
  return new Date().toISOString()
}

const DEPENDENCY_SELECT = `
  SELECT
    td.id, td.task_id, td.depends_on_id,
    t2.title AS depends_on_title,
    t2.current_stage AS depends_on_stage,
    td.required_stage, td.on_cancel_action, td.created_at
  FROM task_dependencies td
  JOIN tasks t2 ON t2.id = td.depends_on_id
`

/**
 * Add a dependency: task_id must wait for depends_on_id to reach required_stage.
 * Throws if the dependency would create a cycle (including self-dependency).
 */
export function addDependency(
  taskId: string,
  dependsOnId: string,
  requiredStage: 'done' | 'cancelled' = 'done',
  onCancelAction: 'cancel' | 'start' | 'on_hold' = 'on_hold',
  db: Database = getDb(),
): TaskDependency {
  if (wouldCreateCycle(taskId, dependsOnId, db))
    throw new Error(`Dependency would create a cycle between ${taskId} and ${dependsOnId}`)
  const id = randomUUID()
  db.prepare(`
    INSERT INTO task_dependencies (id, task_id, depends_on_id, required_stage, on_cancel_action, created_at)
    VALUES (?, ?, ?, ?, ?, ?)
  `).run(id, taskId, dependsOnId, requiredStage, onCancelAction, nowIso())
  return getDependencyById(id, db)!
}

export function removeDependency(taskId: string, dependsOnId: string, db: Database = getDb()): boolean {
  const result = db
    .prepare('DELETE FROM task_dependencies WHERE task_id = ? AND depends_on_id = ?')
    .run(taskId, dependsOnId)
  return result.changes > 0
}

/** Remove a dependency by its row ID (used by the DELETE /dependencies/:depId route). */
export function removeDependencyById(id: string, taskId: string, db: Database = getDb()): boolean {
  const result = db
    .prepare('DELETE FROM task_dependencies WHERE id = ? AND task_id = ?')
    .run(id, taskId)
  return result.changes > 0
}

/** All prerequisites for a task (what this task is waiting for). */
export function getDependenciesFor(taskId: string, db: Database = getDb()): TaskDependency[] {
  const rows = db
    .prepare(`${DEPENDENCY_SELECT} WHERE td.task_id = ?`)
    .all(taskId) as TaskDependencyRow[]
  return rows.map(rowToTaskDependency)
}

/** All tasks that are waiting on this task. */
export function getDependentsOf(taskId: string, db: Database = getDb()): TaskDependency[] {
  const rows = db
    .prepare(`${DEPENDENCY_SELECT} WHERE td.depends_on_id = ?`)
    .all(taskId) as TaskDependencyRow[]
  return rows.map(rowToTaskDependency)
}

/**
 * Returns true if task_id has at least one prerequisite that has not yet
 * reached its required_stage.
 */
export function isBlocked(taskId: string, db: Database = getDb()): boolean {
  const row = db
    .prepare(`
      SELECT 1 FROM task_dependencies td
      JOIN tasks t2 ON t2.id = td.depends_on_id
      WHERE td.task_id = ?
        AND t2.current_stage != td.required_stage
      LIMIT 1
    `)
    .get(taskId)
  return row !== undefined
}

function getDependencyById(id: string, db: Database): TaskDependency | null {
  const row = db
    .prepare(`${DEPENDENCY_SELECT} WHERE td.id = ?`)
    .get(id) as TaskDependencyRow | undefined
  return row ? rowToTaskDependency(row) : null
}

/**
 * DFS from dependsOnId following existing task_id → depends_on_id edges.
 * If the walk reaches taskId, the proposed insert would create a cycle.
 * Also catches self-dependency (taskId === dependsOnId).
 */
function wouldCreateCycle(taskId: string, dependsOnId: string, db: Database): boolean {
  if (taskId === dependsOnId)
    return true
  const visited = new Set<string>()
  const queue = [dependsOnId]
  while (queue.length > 0) {
    const current = queue.shift()!
    if (visited.has(current))
      continue
    visited.add(current)
    const predecessors = db
      .prepare('SELECT depends_on_id FROM task_dependencies WHERE task_id = ?')
      .all(current) as Array<{ depends_on_id: string }>
    for (const row of predecessors) {
      if (row.depends_on_id === taskId)
        return true
      queue.push(row.depends_on_id)
    }
  }
  return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: All `taskDependenciesRepo` tests pass; all existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add server/db/taskDependenciesRepo.ts server/db/db.test.ts
git commit -m "feat: add taskDependenciesRepo with cycle detection"
```

---

### Task 4: `isBlocked` in task queries + `listPickableTasks` excludes blocked tasks

**Files:**
- Modify: `server/db/tasksRepo.ts`
- Modify: `server/db/db.test.ts`

- [ ] **Step 1: Write failing tests in `server/db/db.test.ts`**

Add to the existing `describe('tasksRepo', ...)` block:

```typescript
  it('getTaskById includes isBlocked=true when task has unmet dependency', () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'blk-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'blk-b', title: 'B', cwd: '/b' })
    addDep(b.id, a.id)
    expect(getTaskById(b.id)?.isBlocked).toBe(true)
  })

  it('getTaskById includes isBlocked=false when dependency is met', () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'blk-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'blk-d', title: 'D', cwd: '/d' })
    addDep(b.id, a.id)
    updateTask(a.id, { currentStage: 'done' })
    expect(getTaskById(b.id)?.isBlocked).toBe(false)
  })

  it('listPickableTasks excludes blocked tasks', () => {
    const { addDependency: addDep, listPickableTasks: lpt } = await import('./taskDependenciesRepo.js')
    // Note: listPickableTasks is in tasksRepo, not taskDependenciesRepo
    // Use the import from tasksRepo
  })
```

Wait — the tests for `listPickableTasks` should import from `tasksRepo`. Replace the last test with:

```typescript
  it('listPickableTasks excludes tasks with unmet dependencies', () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'pkb-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'pkb-b', title: 'B', cwd: '/b' })
    addDep(b.id, a.id) // b waits for a (a is still backlog → not done)
    const pickable = listPickableTasks()
    const ids = pickable.map(t => t.id)
    expect(ids).toContain(a.id)   // a has no deps, is pickable
    expect(ids).not.toContain(b.id) // b is blocked
  })

  it('listPickableTasks includes a task once all its deps are met', () => {
    const { addDependency: addDep } = await import('./taskDependenciesRepo.js')
    const a = createTask({ slug: 'pkb-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'pkb-d', title: 'D', cwd: '/d' })
    addDep(b.id, a.id)
    updateTask(a.id, { currentStage: 'done' })
    const pickable = listPickableTasks()
    expect(pickable.map(t => t.id)).toContain(b.id)
  })
```

These tests are `async` — update the `it` call signatures to `it('...', async () => { ... })`.

- [ ] **Step 2: Run failing tests**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: The new `isBlocked` and `listPickableTasks` tests FAIL.

- [ ] **Step 3: Update `server/db/tasksRepo.ts`**

The `IS_BLOCKED_SUBQUERY` constant used in multiple queries (avoids repetition):

After the imports (after line 6), add:

```typescript
const IS_BLOCKED_EXPR = `
  CASE WHEN EXISTS (
    SELECT 1 FROM task_dependencies td
    JOIN tasks t2 ON t2.id = td.depends_on_id
    WHERE td.task_id = tasks.id
      AND t2.current_stage != td.required_stage
  ) THEN 1 ELSE 0 END AS is_blocked
`
```

Update `getTaskById` (line 93):

```typescript
export function getTaskById(id: string, db: Database = getDb()): PipelineTask | null {
  const row = db.prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR} FROM tasks WHERE tasks.id = ?`).get(id) as TaskRow | undefined
  return row ? rowToTask(row) : null
}
```

Update `getTaskBySlug` (line 98):

```typescript
export function getTaskBySlug(slug: string, db: Database = getDb()): PipelineTask | null {
  const row = db.prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR} FROM tasks WHERE tasks.slug = ?`).get(slug) as TaskRow | undefined
  return row ? rowToTask(row) : null
}
```

Update `listTasks` (line 102):

```typescript
export function listTasks(db: Database = getDb()): PipelineTask[] {
  const rows = db.prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR} FROM tasks ORDER BY tasks.created_at DESC`).all() as TaskRow[]
  return rows.map(rowToTask)
}
```

Update `listTasksByStage` (line 106):

```typescript
export function listTasksByStage(stage: PipelineStage, db: Database = getDb()): PipelineTask[] {
  const rows = db
    .prepare(`SELECT tasks.*, ${IS_BLOCKED_EXPR} FROM tasks WHERE tasks.current_stage = ? ORDER BY tasks.created_at DESC`)
    .all(stage) as TaskRow[]
  return rows.map(rowToTask)
}
```

Update `listPickableTasks` (line 118) — add NOT EXISTS filter and the `is_blocked` subquery:

```typescript
export function listPickableTasks(db: Database = getDb()): PipelineTask[] {
  const rows = db
    .prepare(`
      SELECT tasks.*, ${IS_BLOCKED_EXPR} FROM tasks
      WHERE tasks.current_stage NOT IN ('done','cancelled','on_hold','approval1','approval2')
        AND NOT EXISTS (
          SELECT 1 FROM task_dependencies td
          JOIN tasks t2 ON t2.id = td.depends_on_id
          WHERE td.task_id = tasks.id
            AND t2.current_stage != td.required_stage
        )
    `)
    .all() as TaskRow[]
  return rows.map(rowToTask)
}
```

- [ ] **Step 4: Run tests**

```bash
pnpm test -- --reporter=verbose server/db/db.test.ts
```

Expected: All tests pass, including the new `isBlocked` and pickable tests.

- [ ] **Step 5: Run full test suite**

```bash
pnpm test
```

Expected: All 314+ tests pass.

- [ ] **Step 6: Commit**

```bash
git add server/db/tasksRepo.ts server/db/db.test.ts
git commit -m "feat: isBlocked computed field and listPickableTasks excludes blocked tasks"
```

---

### Task 5: Orchestrator `handleDependentTasks` + `notifyTaskTerminated`

**Files:**
- Modify: `server/pipeline/orchestrator.ts`
- Modify: `server/pipeline/orchestrator.test.ts`

- [ ] **Step 1: Write failing tests for `handleDependentTasks`**

Read `server/pipeline/orchestrator.test.ts` first to understand the test helper pattern (`makeOrchestrator`, `makeStubbedOrchestrator`, etc.), then add to the end of that file:

```typescript
describe('handleDependentTasks (dependency cascade)', () => {
  let tmpDir: string

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'orch-dep-test-'))
    process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
    getDb()
  })

  afterEach(() => {
    closeDb()
    rmSync(tmpDir, { recursive: true, force: true })
    delete process.env.DASHBOARD_DB_PATH
  })

  it('when prerequisite reaches done: dependent becomes pickable (no cascade action)', () => {
    const a = createTask({ slug: 'a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'b', title: 'B', cwd: '/b' })
    addDependency(b.id, a.id, 'done', 'cancel')
    expect(isBlocked(b.id)).toBe(true)

    const notified: string[] = []
    const orch = new PipelineOrchestrator({
      onTaskChanged: (taskId) => { notified.push(taskId) },
    })
    orch.notifyTaskTerminated(a.id, 'done')
    updateTask(a.id, { currentStage: 'done' }) // simulate the actual stage change

    expect(isBlocked(b.id)).toBe(false)
    // b is now pickable; no cascade stage change since action only fires on cancelled
    expect(getTaskById(b.id)?.currentStage).toBe('backlog')
  })

  it('when prerequisite cancelled + on_cancel_action=cancel: dependent moves to cancelled', () => {
    const a = createTask({ slug: 'ca', title: 'CA', cwd: '/ca' })
    const b = createTask({ slug: 'cb', title: 'CB', cwd: '/cb' })
    addDependency(b.id, a.id, 'done', 'cancel')
    updateTask(a.id, { currentStage: 'cancelled' })

    const notified: string[] = []
    const orch = new PipelineOrchestrator({
      onTaskChanged: (taskId) => { notified.push(taskId) },
    })
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('cancelled')
    expect(notified).toContain(b.id)
  })

  it('when prerequisite cancelled + on_cancel_action=on_hold: dependent moves to on_hold', () => {
    const a = createTask({ slug: 'ha', title: 'HA', cwd: '/ha' })
    const b = createTask({ slug: 'hb', title: 'HB', cwd: '/hb' })
    addDependency(b.id, a.id, 'done', 'on_hold')
    updateTask(a.id, { currentStage: 'cancelled' })

    const orch = new PipelineOrchestrator({})
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('on_hold')
  })

  it('when prerequisite cancelled + on_cancel_action=start: dependent stays pickable (no stage change)', () => {
    const a = createTask({ slug: 'sa', title: 'SA', cwd: '/sa' })
    const b = createTask({ slug: 'sb', title: 'SB', cwd: '/sb' })
    addDependency(b.id, a.id, 'done', 'start')
    updateTask(a.id, { currentStage: 'cancelled' })

    const orch = new PipelineOrchestrator({})
    orch.notifyTaskTerminated(a.id, 'cancelled')

    expect(getTaskById(b.id)?.currentStage).toBe('backlog')
  })

  it('when prerequisite cancelled but dependent still has other unmet deps: no cascade', () => {
    const a = createTask({ slug: 'ma', title: 'MA', cwd: '/ma' })
    const b = createTask({ slug: 'mb', title: 'MB', cwd: '/mb' })
    const c = createTask({ slug: 'mc', title: 'MC', cwd: '/mc' })
    addDependency(c.id, a.id, 'done', 'cancel')
    addDependency(c.id, b.id, 'done', 'cancel')
    updateTask(a.id, { currentStage: 'cancelled' })

    const orch = new PipelineOrchestrator({})
    orch.notifyTaskTerminated(a.id, 'cancelled')

    // c still blocked by b (not done/cancelled), so no cascade
    expect(getTaskById(c.id)?.currentStage).toBe('backlog')
  })
})
```

The test file needs these additional imports at the top:

```typescript
import { addDependency, isBlocked } from '../db/taskDependenciesRepo.js'
import { createTask, getTaskById, updateTask } from '../db/tasksRepo.js'
import { closeDb, getDb } from '../db/client.js'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
```

- [ ] **Step 2: Run failing tests**

```bash
pnpm test -- --reporter=verbose server/pipeline/orchestrator.test.ts
```

Expected: The `handleDependentTasks` tests FAIL with "orch.notifyTaskTerminated is not a function".

- [ ] **Step 3: Add `handleDependentTasks` and `notifyTaskTerminated` to `server/pipeline/orchestrator.ts`**

Add these imports at the top (after the existing imports):

```typescript
import { getDependentsOf, isBlocked as isDependencyBlocked } from '../db/taskDependenciesRepo.js'
```

Add the private method `handleDependentTasks` and public method `notifyTaskTerminated` inside the `PipelineOrchestrator` class, before the closing `}` of the class:

```typescript
  /**
   * After a task reaches a terminal stage, check if any dependents are now
   * unblocked and apply the configured on_cancel_action for cancelled predecessors.
   * Must be called AFTER the task's stage has already been persisted to the DB.
   */
  private handleDependentTasks(taskId: string, newStage: 'done' | 'cancelled'): void {
    const dependents = getDependentsOf(taskId)
    for (const dep of dependents) {
      // Only act if this is the last blocking dependency
      if (isDependencyBlocked(dep.taskId))
        continue

      if (newStage === 'cancelled') {
        switch (dep.onCancelAction) {
          case 'cancel':
            updateTask(dep.taskId, { currentStage: 'cancelled' })
            this.onTaskChanged?.(dep.taskId, { transitionKind: 'dependency_cancelled_cascade' })
            break
          case 'on_hold':
            updateTask(dep.taskId, { currentStage: 'on_hold' })
            this.onTaskChanged?.(dep.taskId, { transitionKind: 'dependency_on_hold_cascade' })
            break
          case 'start':
            // No stage change needed — the task becomes pickable now that isBlocked = false
            this.onTaskChanged?.(dep.taskId, { transitionKind: 'dependency_unblocked' })
            break
        }
      }
      else {
        // newStage === 'done': dependent is now unblocked, will be picked up on next tick
        this.onTaskChanged?.(dep.taskId, { transitionKind: 'dependency_unblocked' })
      }
    }
  }

  /**
   * Called by the cancel route after updateTask sets currentStage to 'cancelled'.
   * The orchestrator's applyTransition handles the 'done' case internally.
   */
  public notifyTaskTerminated(taskId: string, stage: 'done' | 'cancelled'): void {
    this.handleDependentTasks(taskId, stage)
  }
```

In `applyTransition`, find the `case 'done':` block and add a call to `handleDependentTasks` AFTER the transaction commits. The current `done` case ends with `result = { updatedRunId: stageRun.id, newRunId: null }`. 

The transaction is executed via `txn()` — find where `txn()` is invoked after the switch statement and add the `handleDependentTasks` call after it. The pattern in the file is:

```typescript
    const final = txn()
    this.onTaskChanged?.(task.id, { transitionKind: transition.kind })
```

Change it to:

```typescript
    const final = txn()
    this.onTaskChanged?.(task.id, { transitionKind: transition.kind })
    if (transition.kind === 'done')
      this.handleDependentTasks(task.id, 'done')
```

Read the actual end of `applyTransition` to find the exact lines before making this edit.

- [ ] **Step 4: Read the end of `applyTransition` in `server/pipeline/orchestrator.ts`**

Search for the end of `applyTransition` where `txn()` is called and `onTaskChanged` is invoked:

```bash
grep -n "onTaskChanged\|txn()\|return final" server/pipeline/orchestrator.ts | head -20
```

Find the exact lines and apply the edit at the right position.

- [ ] **Step 5: Run tests**

```bash
pnpm test -- --reporter=verbose server/pipeline/orchestrator.test.ts
```

Expected: All `handleDependentTasks` tests pass; all existing orchestrator tests still pass.

- [ ] **Step 6: Run full test suite**

```bash
pnpm test
```

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add server/pipeline/orchestrator.ts server/pipeline/orchestrator.test.ts
git commit -m "feat: orchestrator handleDependentTasks and notifyTaskTerminated"
```

---

### Task 6: REST API routes (4 dependency endpoints + cancel route hook)

**Files:**
- Modify: `server/routes/taskRoutes.ts`
- Modify: `server/routes/taskRoutes.test.ts`

- [ ] **Step 1: Write failing tests in `server/routes/taskRoutes.test.ts`**

Read `server/routes/taskRoutes.test.ts` to understand the test helper (how Express app is set up, how `supertest` is used). Then add a new describe block at the end:

```typescript
describe('dependency routes', () => {
  it('POST /tasks/:id/dependencies adds a dependency', async () => {
    const a = createTask({ slug: 'drt-a', title: 'A', cwd: '/a' })
    const b = createTask({ slug: 'drt-b', title: 'B', cwd: '/b' })
    const res = await request(app)
      .post(`/api/tasks/${b.id}/dependencies`)
      .send({ dependsOnId: a.id, requiredStage: 'done', onCancelAction: 'cancel' })
    expect(res.status).toBe(201)
    expect(res.body.dependsOnId).toBe(a.id)
  })

  it('POST /tasks/:id/dependencies returns 400 on cycle', async () => {
    const a = createTask({ slug: 'drt-c', title: 'C', cwd: '/c' })
    const b = createTask({ slug: 'drt-d', title: 'D', cwd: '/d' })
    await request(app)
      .post(`/api/tasks/${b.id}/dependencies`)
      .send({ dependsOnId: a.id })
    const res = await request(app)
      .post(`/api/tasks/${a.id}/dependencies`)
      .send({ dependsOnId: b.id })
    expect(res.status).toBe(400)
    expect(res.body.error).toMatch(/cycle/)
  })

  it('GET /tasks/:id/dependencies lists prerequisites', async () => {
    const a = createTask({ slug: 'drt-e', title: 'E', cwd: '/e' })
    const b = createTask({ slug: 'drt-f', title: 'F', cwd: '/f' })
    addDependency(b.id, a.id)
    const res = await request(app).get(`/api/tasks/${b.id}/dependencies`)
    expect(res.status).toBe(200)
    expect(res.body).toHaveLength(1)
    expect(res.body[0].dependsOnId).toBe(a.id)
  })

  it('GET /tasks/:id/dependents lists dependents', async () => {
    const a = createTask({ slug: 'drt-g', title: 'G', cwd: '/g' })
    const b = createTask({ slug: 'drt-h', title: 'H', cwd: '/h' })
    addDependency(b.id, a.id)
    const res = await request(app).get(`/api/tasks/${a.id}/dependents`)
    expect(res.status).toBe(200)
    expect(res.body).toHaveLength(1)
    expect(res.body[0].taskId).toBe(b.id)
  })

  it('DELETE /tasks/:id/dependencies/:depId removes a dependency', async () => {
    const a = createTask({ slug: 'drt-i', title: 'I', cwd: '/i' })
    const b = createTask({ slug: 'drt-j', title: 'J', cwd: '/j' })
    const dep = addDependency(b.id, a.id)
    const res = await request(app)
      .delete(`/api/tasks/${b.id}/dependencies/${dep.id}`)
    expect(res.status).toBe(200)
    expect(res.body.removed).toBe(true)
  })
})
```

Also add `addDependency` to the imports from `taskDependenciesRepo` in the test file.

- [ ] **Step 2: Run failing tests**

```bash
pnpm test -- --reporter=verbose server/routes/taskRoutes.test.ts
```

Expected: Dependency route tests FAIL with 404 (routes don't exist yet).

- [ ] **Step 3: Add dependency routes to `server/routes/taskRoutes.ts`**

First, add imports at the top (after the existing `tasksRepo` imports):

```typescript
import {
  addDependency,
  getDependenciesFor,
  getDependentsOf,
  removeDependencyById,
} from '../db/taskDependenciesRepo.js'
```

Add the four new route handlers after the `GET /tasks/:id/feedback` route. In the router, find the location just before `GET /pipeline/config` and insert:

```typescript
  // GET /tasks/:id/dependencies — list all prerequisites for a task
  router.get('/tasks/:id/dependencies', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(getDependenciesFor(req.params.id))
  })

  // GET /tasks/:id/dependents — list all tasks waiting on this task
  router.get('/tasks/:id/dependents', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    res.json(getDependentsOf(req.params.id))
  })

  // POST /tasks/:id/dependencies — add a dependency
  router.post('/tasks/:id/dependencies', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const { dependsOnId, requiredStage = 'done', onCancelAction = 'on_hold' } = req.body as {
      dependsOnId?: string
      requiredStage?: 'done' | 'cancelled'
      onCancelAction?: 'cancel' | 'start' | 'on_hold'
    }
    if (!dependsOnId) {
      res.status(400).json({ error: 'dependsOnId is required' })
      return
    }
    if (!getTaskById(dependsOnId)) {
      res.status(404).json({ error: `Prerequisite task not found: ${dependsOnId}` })
      return
    }
    try {
      const dep = addDependency(req.params.id, dependsOnId, requiredStage, onCancelAction)
      broadcastEnrichedUpdate(req.params.id)
      res.status(201).json(dep)
    }
    catch (err) {
      const msg = (err as Error).message
      if (msg.includes('cycle')) {
        res.status(400).json({ error: msg })
        return
      }
      if (msg.includes('UNIQUE')) {
        res.status(409).json({ error: 'Dependency already exists' })
        return
      }
      throw err
    }
  })

  // DELETE /tasks/:id/dependencies/:depId — remove a dependency by its row ID
  router.delete('/tasks/:id/dependencies/:depId', (req, res) => {
    const removed = removeDependencyById(req.params.depId, req.params.id)
    if (!removed) {
      res.status(404).json({ error: 'Dependency not found' })
      return
    }
    broadcastEnrichedUpdate(req.params.id)
    res.json({ removed })
  })
```

Also add `import { getDb } from '../db/client.js'` if not already imported (check the top of `taskRoutes.ts` — it is likely already there via the worktreeManager or another import chain).

**Update the cancel route** to call `orchestrator.notifyTaskTerminated`. Find the cancel route handler (searches for `/tasks/:id/cancel`) and add the call after `updateTask`:

```typescript
  router.post('/tasks/:id/cancel', (req, res) => {
    if (rejectCrossOrigin(req, res)) return
    const task = getTaskById(req.params.id)
    if (!task) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    updateTask(req.params.id, { currentStage: 'cancelled' })
    deps.orchestrator.notifyTaskTerminated(req.params.id, 'cancelled')  // ← ADD THIS LINE
    appendAudit({ taskId: req.params.id, actor: 'user', action: 'cancelled' })
    broadcastEnrichedUpdate(req.params.id)
    res.json({ success: true })
  })
```

- [ ] **Step 4: Run tests**

```bash
pnpm test -- --reporter=verbose server/routes/taskRoutes.test.ts
```

Expected: All dependency route tests pass; all existing route tests still pass.

- [ ] **Step 5: Run full test suite**

```bash
pnpm test
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add server/routes/taskRoutes.ts server/routes/taskRoutes.test.ts
git commit -m "feat: dependency REST endpoints and cancel route notifyTaskTerminated hook"
```

---

### Task 7: MCP tools — `add_dependency` + `remove_dependency`

**Files:**
- Create: `server/mcp/tools/dependencyTools.ts`
- Modify: `server/mcp/mcpAuth.ts`
- Modify: `server/mcp/mcpServer.ts`
- Modify: `server/mcp/mcpServer.test.ts`

- [ ] **Step 1: Write failing test in `server/mcp/mcpServer.test.ts`**

Read `server/mcp/mcpServer.test.ts` to understand how tools are tested (likely uses `listTools()` to check registration). Add:

```typescript
  it('registers add_dependency and remove_dependency tools', async () => {
    const tools = await server.listTools()
    const names = tools.tools.map((t: { name: string }) => t.name)
    expect(names).toContain('add_dependency')
    expect(names).toContain('remove_dependency')
  })
```

- [ ] **Step 2: Run failing test**

```bash
pnpm test -- --reporter=verbose server/mcp/mcpServer.test.ts
```

Expected: Test FAILS because tools aren't registered yet.

- [ ] **Step 3: Add `add_dependency` and `remove_dependency` to `TOOL_SCOPE_MAP` in `server/mcp/mcpAuth.ts`**

In the `TOOL_SCOPE_MAP` constant, add to the `tasks:write` section:

```typescript
  // tasks:write
  create_task: 'tasks:write',
  update_task: 'tasks:write',
  delete_task: 'tasks:write',
  add_dependency: 'tasks:write',
  remove_dependency: 'tasks:write',
```

- [ ] **Step 4: Create `server/mcp/tools/dependencyTools.ts`**

```typescript
import { z } from 'zod'
import { getTaskById } from '../../db/tasksRepo.js'
import {
  addDependency,
  removeDependency,
} from '../../db/taskDependenciesRepo.js'
import { mcpError, ok } from '../mcpAuth.js'
import type { makeToolRegistrar } from '../mcpAuth.js'

type ToolFn = ReturnType<typeof makeToolRegistrar>

export function registerDependencyTools(tool: ToolFn, broadcast: (taskId: string) => void): void {
  tool(
    'add_dependency',
    {
      task_id: z.string().describe('ID of the dependent task (the one that waits)'),
      depends_on_id: z.string().describe('ID of the prerequisite task'),
      required_stage: z.enum(['done', 'cancelled']).default('done'),
      on_cancel_action: z.enum(['cancel', 'start', 'on_hold']).default('on_hold')
        .describe('What to do with this task when the prerequisite is cancelled'),
    },
    async ({ task_id, depends_on_id, required_stage, on_cancel_action }) => {
      if (!getTaskById(task_id))
        mcpError(`Task not found: ${task_id}`)
      if (!getTaskById(depends_on_id))
        mcpError(`Prerequisite task not found: ${depends_on_id}`)
      try {
        const dep = addDependency(task_id, depends_on_id, required_stage, on_cancel_action)
        broadcast(task_id)
        return ok(dep)
      }
      catch (err) {
        mcpError((err as Error).message)
      }
    },
  )

  tool(
    'remove_dependency',
    {
      task_id: z.string().describe('ID of the dependent task'),
      depends_on_id: z.string().describe('ID of the prerequisite task to remove'),
    },
    async ({ task_id, depends_on_id }) => {
      const removed = removeDependency(task_id, depends_on_id)
      if (removed)
        broadcast(task_id)
      return ok({ removed })
    },
  )
}
```

- [ ] **Step 5: Register in `server/mcp/mcpServer.ts`**

Add import:

```typescript
import { registerDependencyTools } from './tools/dependencyTools.js'
```

In `buildMcpServer`, add after the existing `registerWriteTools` call:

```typescript
  registerDependencyTools(tool, broadcast)
```

The `broadcast` parameter is already available in `buildMcpServer` — it's the same one passed to `registerWriteTools`.

- [ ] **Step 6: Run tests**

```bash
pnpm test -- --reporter=verbose server/mcp/mcpServer.test.ts server/mcp/mcpAuth.test.ts
```

Expected: All tests pass including the new tool registration test.

- [ ] **Step 7: Run full test suite**

```bash
pnpm test
```

Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add server/mcp/tools/dependencyTools.ts server/mcp/mcpAuth.ts server/mcp/mcpServer.ts server/mcp/mcpServer.test.ts
git commit -m "feat: add_dependency and remove_dependency MCP tools"
```

---

### Task 8: TaskCard — blocked badge + dim styling

**Files:**
- Modify: `src/components/TaskCard.vue`

- [ ] **Step 1: Update the `<template>` in `src/components/TaskCard.vue`**

In the `task-meta` div (line 65), add a blocked badge after the `parentTaskId` chip:

```html
      <span v-if="task.isBlocked" class="meta-chip blocked" title="Waiting for prerequisite tasks">🔒 Blocked</span>
```

Also add a `blocked` CSS class to the root `.task-card` div:

```html
  <div
    class="task-card"
    :class="{ 'is-blocked': task.isBlocked }"
    ...
  >
```

- [ ] **Step 2: Add CSS for `.is-blocked` and `.meta-chip.blocked` in `<style scoped>`**

After the `.meta-chip.run-status.run-failed` rule (after line 220), add:

```css
.meta-chip.blocked {
  background: rgba(148, 163, 184, 0.15);
  color: var(--text-muted);
  border: 1px solid rgba(148, 163, 184, 0.3);
}
.task-card.is-blocked {
  opacity: 0.6;
}
.task-card.is-blocked:hover {
  opacity: 0.85;
  border-color: var(--border);
  transform: none;
}
```

- [ ] **Step 3: Start dev server and visually verify**

```bash
pnpm dev
```

Open `http://localhost:13120`. Create two tasks and add a dependency via the REST API:

```bash
# Create prerequisite (task A) and dependent (task B), then add dependency
curl -s -X POST http://localhost:13120/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"slug":"prereq","title":"Prerequisite Task","cwd":"/tmp"}'

curl -s -X POST http://localhost:13120/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"slug":"dependent","title":"Dependent Task","cwd":"/tmp"}'
```

Get the task IDs from the responses, then:

```bash
curl -s -X POST http://localhost:13120/api/tasks/<B_ID>/dependencies \
  -H "Content-Type: application/json" \
  -d '{"dependsOnId":"<A_ID>"}'
```

Verify: Task B card shows "🔒 Blocked" badge and appears dimmed in the board.

- [ ] **Step 4: Commit**

```bash
git add src/components/TaskCard.vue
git commit -m "feat: TaskCard blocked badge and dim styling"
```

---

### Task 9: TaskModal — dependencies section + composable API functions

**Files:**
- Modify: `src/composables/useTasks.ts`
- Modify: `src/components/TaskModal.vue`

- [ ] **Step 1: Add dependency API functions to `src/composables/useTasks.ts`**

Read the file to find the export pattern (likely `export async function fetchStageRuns(...)` etc.). Add four new exports following the same pattern:

```typescript
export async function fetchDependencies(taskId: string): Promise<TaskDependency[]> {
  const res = await fetch(`/api/tasks/${taskId}/dependencies`)
  if (!res.ok) throw new Error(await res.text())
  return res.json() as Promise<TaskDependency[]>
}

export async function fetchDependents(taskId: string): Promise<TaskDependency[]> {
  const res = await fetch(`/api/tasks/${taskId}/dependents`)
  if (!res.ok) throw new Error(await res.text())
  return res.json() as Promise<TaskDependency[]>
}

export async function addTaskDependency(
  taskId: string,
  dependsOnId: string,
  requiredStage: 'done' | 'cancelled' = 'done',
  onCancelAction: 'cancel' | 'start' | 'on_hold' = 'on_hold',
): Promise<TaskDependency> {
  const res = await fetch(`/api/tasks/${taskId}/dependencies`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ dependsOnId, requiredStage, onCancelAction }),
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json() as Promise<TaskDependency>
}

export async function removeTaskDependency(taskId: string, depId: string): Promise<void> {
  const res = await fetch(`/api/tasks/${taskId}/dependencies/${depId}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(await res.text())
}
```

Add `TaskDependency` to the type imports at the top of `useTasks.ts`.

- [ ] **Step 2: Add dependencies section to `src/components/TaskModal.vue`**

First read `src/components/TaskModal.vue` lines 80-200 to understand the tab structure and where to add the section.

Add reactive state for dependencies at the top of `<script setup>`:

```typescript
import {
  addTaskDependency,
  fetchDependencies,
  fetchDependents,
  removeTaskDependency,
} from '../composables/useTasks'
import type { TaskDependency } from '../types'

const dependencies = ref<TaskDependency[]>([])
const dependents = ref<TaskDependency[]>([])
const newDepId = ref('')
const newDepStage = ref<'done' | 'cancelled'>('done')
const newDepCancelAction = ref<'cancel' | 'start' | 'on_hold'>('on_hold')
const depError = ref('')
const isAddingDep = ref(false)

async function loadDependencies(): Promise<void> {
  if (!props.task) return
  const [deps, depts] = await Promise.all([
    fetchDependencies(props.task.id),
    fetchDependents(props.task.id),
  ])
  dependencies.value = deps
  dependents.value = depts
}

async function handleAddDependency(): Promise<void> {
  if (!props.task || !newDepId.value.trim()) return
  depError.value = ''
  isAddingDep.value = true
  try {
    await addTaskDependency(props.task.id, newDepId.value.trim(), newDepStage.value, newDepCancelAction.value)
    newDepId.value = ''
    await loadDependencies()
  }
  catch (err) {
    depError.value = (err as Error).message
  }
  finally {
    isAddingDep.value = false
  }
}

async function handleRemoveDependency(depId: string): Promise<void> {
  if (!props.task) return
  await removeTaskDependency(props.task.id, depId)
  await loadDependencies()
}
```

Call `loadDependencies()` inside the existing `watch(props.task, ...)` or `onMounted` to load when modal opens:

```typescript
watch(() => props.task, (task) => {
  if (task) {
    void loadDependencies()
  }
}, { immediate: true })
```

Add the dependencies section in the template in the `overview` tab section (or as a new section after the existing overview content). Find the area in the template where the overview tab content ends and add:

```html
<!-- Dependencies section -->
<section v-if="activeTab === 'overview'" class="dep-section">
  <h4 class="dep-heading">Abhängigkeiten</h4>

  <div v-if="dependencies.length > 0" class="dep-list">
    <p class="dep-subheading">Wartet auf:</p>
    <div
      v-for="dep in dependencies"
      :key="dep.id"
      class="dep-row"
    >
      <span class="dep-title">{{ dep.dependsOnTitle }}</span>
      <span
        class="meta-chip stage"
        :class="dep.dependsOnStage === dep.requiredStage ? 'dep-met' : 'dep-unmet'"
      >{{ dep.dependsOnStage }}</span>
      <span class="dep-action-hint">on cancel: {{ dep.onCancelAction }}</span>
      <button class="dep-remove" title="Remove dependency" @click="handleRemoveDependency(dep.id)">✕</button>
    </div>
  </div>

  <div v-if="dependents.length > 0" class="dep-list">
    <p class="dep-subheading">Wird benötigt von:</p>
    <div v-for="dep in dependents" :key="dep.id" class="dep-row">
      <span class="dep-title">{{ dep.dependsOnTitle || dep.taskId }}</span>
    </div>
  </div>

  <form class="dep-add-form" @submit.prevent="handleAddDependency">
    <input
      v-model="newDepId"
      class="dep-input"
      placeholder="Vorgänger Task-ID"
      :disabled="isAddingDep"
    />
    <select v-model="newDepStage" class="dep-select">
      <option value="done">Done</option>
      <option value="cancelled">Cancelled</option>
    </select>
    <select v-model="newDepCancelAction" class="dep-select">
      <option value="on_hold">On Hold (bei Cancel)</option>
      <option value="cancel">Cancel (bei Cancel)</option>
      <option value="start">Start (bei Cancel)</option>
    </select>
    <button class="dep-add-btn" type="submit" :disabled="isAddingDep || !newDepId.trim()">
      Hinzufügen
    </button>
  </form>
  <p v-if="depError" class="dep-error">{{ depError }}</p>
</section>
```

Add the corresponding `<style scoped>` rules:

```css
.dep-section {
  margin-top: 16px;
  border-top: 1px solid var(--border);
  padding-top: 12px;
}
.dep-heading {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0 0 8px;
}
.dep-subheading {
  font-size: 11px;
  color: var(--text-muted);
  margin: 0 0 4px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.dep-list {
  margin-bottom: 10px;
}
.dep-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
}
.dep-title {
  flex: 1;
  color: var(--text-primary);
}
.dep-action-hint {
  font-size: 10px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.dep-met {
  background: rgba(74, 222, 128, 0.15);
  color: var(--accent-green);
  border: 1px solid var(--accent-green);
}
.dep-unmet {
  background: rgba(248, 113, 113, 0.15);
  color: var(--accent-red);
  border: 1px solid rgba(248, 113, 113, 0.5);
}
.dep-remove {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted);
  padding: 2px 4px;
  font-size: 10px;
  border-radius: 3px;
}
.dep-remove:hover {
  background: rgba(248, 113, 113, 0.15);
  color: var(--accent-red);
}
.dep-add-form {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
  margin-top: 8px;
}
.dep-input {
  flex: 1;
  min-width: 0;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 12px;
}
.dep-select {
  padding: 4px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 11px;
}
.dep-add-btn {
  padding: 4px 10px;
  background: var(--accent-blue);
  color: white;
  border: none;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
}
.dep-add-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.dep-error {
  font-size: 11px;
  color: var(--accent-red);
  margin: 4px 0 0;
}
```

- [ ] **Step 3: Verify in browser**

With `pnpm dev` running, open a task modal and verify:
- The "Abhängigkeiten" section is visible in the overview tab
- Adding a dependency via the form works and updates the list
- The lock badge appears on blocked cards after adding a dependency
- Completing a prerequisite task removes the blocked state on the dependent

- [ ] **Step 4: Run typecheck**

```bash
pnpm typecheck
```

Expected: No errors.

- [ ] **Step 5: Run full test suite**

```bash
pnpm test
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add src/composables/useTasks.ts src/components/TaskModal.vue
git commit -m "feat: TaskModal dependencies section and useTasks API helpers"
```
