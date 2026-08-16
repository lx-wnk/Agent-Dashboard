# Phase 3 — Developer UX & Security Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce daily friction: git panel in TaskModal, slash commands in task prompt input, permission re-request transparency on kanban, audit log tab implementation, Stripe-compatible webhook signing.

**Architecture:** Git panel uses a new thin Express endpoint shelling out via execFile (same pattern as worktreeManager). Slash commands extend the existing `PromptInput.vue` pattern into the task domain — a new `TaskSlashCommandMenu` component wired to TaskModal's `additionalPrompt` textarea. Webhook HMAC adds an opt-in signing layer to the existing adapter. Audit log route already exists — the tab needs its component implementation. Permission re-request count is a new DB query + badge.

**Tech Stack:** Vue 3 Composition API, node:child_process execFile, crypto.createHmac, Vitest

---

## Task 1: Git status panel for task worktrees (DX-1)

**Files:**
- `server/services/gitService.ts` (new)
- `server/routes/taskRoutes.ts` (add two routes)
- `src/components/GitStatusPanel.vue` (new)
- `src/components/TaskModal.vue` (wire panel into Overview tab)
- `src/types.ts` (add `GitStatus` interface)

### Steps

- [ ] **1.1 — Define `GitStatus` type in `src/types.ts`**

  Add after the existing `SubAgent` interface:

  ```typescript
  export interface GitStatusLastCommit {
    hash: string
    shortHash: string
    message: string
    author: string
    date: string
  }

  export interface GitStatus {
    branch: string
    aheadCount: number
    behindCount: number
    staged: string[]
    unstaged: string[]
    untracked: string[]
    lastCommit: GitStatusLastCommit | null
    remoteUrl: string | null
  }
  ```

- [ ] **1.2 — Create `server/services/gitService.ts`**

  ```typescript
  import { execFile as _execFile } from 'node:child_process'
  import { promisify } from 'node:util'
  import type { GitStatus } from '../../src/types.js'

  const execFile = promisify(_execFile)

  const GIT_TIMEOUT_MS = 10_000

  async function git(cwd: string, args: string[]): Promise<string> {
    const { stdout } = await execFile('git', args, {
      cwd,
      timeout: GIT_TIMEOUT_MS,
      maxBuffer: 1024 * 512,
    })
    return stdout.trim()
  }

  export async function getGitStatus(cwd: string): Promise<GitStatus> {
    // Branch
    const branch = await git(cwd, ['rev-parse', '--abbrev-ref', 'HEAD']).catch(() => 'HEAD')

    // Ahead/behind vs. upstream
    let aheadCount = 0
    let behindCount = 0
    try {
      const ab = await git(cwd, ['rev-list', '--left-right', '--count', '@{u}...HEAD'])
      const parts = ab.split(/\s+/)
      behindCount = Number(parts[0]) || 0
      aheadCount = Number(parts[1]) || 0
    }
    catch {
      // No upstream configured — leave 0/0
    }

    // Staged / unstaged / untracked
    const statusOut = await git(cwd, ['status', '--porcelain=v1']).catch(() => '')
    const staged: string[] = []
    const unstaged: string[] = []
    const untracked: string[] = []
    for (const line of statusOut.split('\n').filter(Boolean)) {
      const xy = line.slice(0, 2)
      const file = line.slice(3)
      if (xy[0] !== ' ' && xy[0] !== '?')
        staged.push(file)
      if (xy[1] !== ' ' && xy[1] !== '?')
        unstaged.push(file)
      if (xy === '??')
        untracked.push(file)
    }

    // Last commit
    let lastCommit: GitStatus['lastCommit'] = null
    try {
      const logLine = await git(cwd, [
        'log',
        '-1',
        '--format=%H%x00%h%x00%s%x00%an%x00%aI',
      ])
      if (logLine) {
        const [hash, shortHash, message, author, date] = logLine.split('\x00')
        lastCommit = { hash, shortHash, message, author, date }
      }
    }
    catch {
      // No commits yet
    }

    // Remote URL
    let remoteUrl: string | null = null
    try {
      remoteUrl = await git(cwd, ['remote', 'get-url', 'origin'])
    }
    catch {
      // No remote
    }

    return { branch, aheadCount, behindCount, staged, unstaged, untracked, lastCommit, remoteUrl }
  }

  export type GitAction = 'fetch' | 'pull' | 'log'

  export async function runGitAction(cwd: string, action: GitAction): Promise<string> {
    switch (action) {
      case 'fetch':
        return git(cwd, ['fetch', '--prune'])
      case 'pull':
        return git(cwd, ['pull', '--ff-only'])
      case 'log':
        return git(cwd, ['log', '--oneline', '-20'])
      default:
        throw new Error(`Unknown git action: ${action}`)
    }
  }
  ```

- [ ] **1.3 — Add routes to `server/routes/taskRoutes.ts`**

  Add near the end of the router, before the closing export, after the audit route block:

  ```typescript
  // ─── Git ────────────────────────────────────────────────────────────────────

  router.get('/tasks/:id/git', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const cwd = task.worktreePath ?? task.cwd
    if (!cwd) {
      res.status(409).json({ error: 'Task has no worktree or cwd' })
      return
    }
    try {
      const status = await getGitStatus(cwd)
      res.json(status)
    }
    catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      if (msg.includes('ENOENT') || msg.includes('not found')) {
        res.status(503).json({ error: 'git CLI not found' })
      }
      else {
        res.status(500).json({ error: msg })
      }
    }
  })

  router.post('/tasks/:id/git/actions', async (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const { action } = req.body as { action: string }
    if (!['fetch', 'pull', 'log'].includes(action)) {
      res.status(400).json({ error: 'Invalid action. Allowed: fetch, pull, log' })
      return
    }
    if (action === 'pull' && process.env.DASHBOARD_ALLOW_GIT_PULL !== 'true') {
      res.status(403).json({ error: 'pull is disabled. Set DASHBOARD_ALLOW_GIT_PULL=true to enable.' })
      return
    }
    const cwd = task.worktreePath ?? task.cwd
    if (!cwd) {
      res.status(409).json({ error: 'Task has no worktree or cwd' })
      return
    }
    try {
      const output = await runGitAction(cwd, action as 'fetch' | 'pull' | 'log')
      res.json({ output })
    }
    catch (err: unknown) {
      res.status(500).json({ error: err instanceof Error ? err.message : String(err) })
    }
  })
  ```

  Add the import at the top of the file alongside the other service imports:

  ```typescript
  import { getGitStatus, runGitAction } from '../services/gitService.js'
  ```

- [ ] **1.4 — Create `src/components/GitStatusPanel.vue`**

  ```vue
  <script setup lang="ts">
  import type { GitStatus } from '../types'
  import { ref } from 'vue'

  const props = defineProps<{ taskId: string }>()

  const status = ref<GitStatus | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const actionOutput = ref<string | null>(null)
  const actionLoading = ref(false)

  async function load() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch(`/api/tasks/${props.taskId}/git`)
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }))
        error.value = body.error ?? res.statusText
        return
      }
      status.value = await res.json()
    }
    catch (e) {
      error.value = e instanceof Error ? e.message : 'Network error'
    }
    finally {
      loading.value = false
    }
  }

  async function runAction(action: 'fetch' | 'pull' | 'log') {
    actionLoading.value = true
    actionOutput.value = null
    try {
      const res = await fetch(`/api/tasks/${props.taskId}/git/actions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      })
      const body = await res.json()
      if (!res.ok) {
        error.value = body.error ?? res.statusText
        return
      }
      actionOutput.value = body.output
      if (action === 'fetch')
        await load()
    }
    catch (e) {
      error.value = e instanceof Error ? e.message : 'Network error'
    }
    finally {
      actionLoading.value = false
    }
  }

  // Load on mount
  load()
  </script>

  <template>
    <div class="rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 p-4 text-xs">
      <div class="flex items-center justify-between mb-3">
        <span class="font-semibold text-slate-700 dark:text-slate-300 text-[13px]">Git</span>
        <div class="flex gap-1.5">
          <button
            type="button"
            class="px-2 py-0.5 rounded bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:brightness-110 disabled:opacity-40"
            :disabled="loading || actionLoading"
            @click="load"
          >
            Refresh
          </button>
          <button
            type="button"
            class="px-2 py-0.5 rounded bg-blue-600 text-white hover:brightness-110 disabled:opacity-40"
            :disabled="loading || actionLoading"
            @click="runAction('fetch')"
          >
            Fetch
          </button>
        </div>
      </div>

      <div v-if="loading" class="text-slate-400 dark:text-slate-600 text-center py-4">Loading…</div>
      <div v-else-if="error" class="text-red-600 dark:text-red-400">{{ error }}</div>

      <template v-else-if="status">
        <!-- Branch + ahead/behind -->
        <div class="flex items-center gap-2 mb-2">
          <span class="font-mono text-blue-600 dark:text-blue-400">{{ status.branch }}</span>
          <span
            v-if="status.aheadCount > 0"
            class="px-1.5 py-0.5 rounded-full text-[10px] bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-400"
          >↑{{ status.aheadCount }}</span>
          <span
            v-if="status.behindCount > 0"
            class="px-1.5 py-0.5 rounded-full text-[10px] bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400"
          >↓{{ status.behindCount }}</span>
        </div>

        <!-- File counts -->
        <div class="flex gap-3 mb-2 text-[11px]">
          <span v-if="status.staged.length > 0" class="text-green-600 dark:text-green-400">
            +{{ status.staged.length }} staged
          </span>
          <span v-if="status.unstaged.length > 0" class="text-amber-600 dark:text-amber-400">
            ~{{ status.unstaged.length }} unstaged
          </span>
          <span v-if="status.untracked.length > 0" class="text-slate-500 dark:text-slate-400">
            ?{{ status.untracked.length }} untracked
          </span>
          <span
            v-if="status.staged.length === 0 && status.unstaged.length === 0 && status.untracked.length === 0"
            class="text-slate-400 dark:text-slate-600"
          >
            clean
          </span>
        </div>

        <!-- Last commit -->
        <div v-if="status.lastCommit" class="font-mono text-slate-500 dark:text-slate-400 truncate">
          <span class="text-slate-400 dark:text-slate-600 mr-1">{{ status.lastCommit.shortHash }}</span>
          {{ status.lastCommit.message }}
        </div>
      </template>

      <!-- Action output -->
      <pre
        v-if="actionOutput"
        class="mt-3 bg-slate-100 dark:bg-slate-800 rounded p-2 text-[11px] font-mono overflow-x-auto whitespace-pre-wrap max-h-32 overflow-y-auto"
      >{{ actionOutput }}</pre>
    </div>
  </template>
  ```

- [ ] **1.5 — Wire `GitStatusPanel` into `TaskModal.vue` Overview tab**

  Add import at top of `<script setup>` in `TaskModal.vue`:

  ```typescript
  import GitStatusPanel from './GitStatusPanel.vue'
  ```

  In the Overview tab section (`v-if="activeTab === 'overview'"`), add after the last existing card (e.g. after the `CrossLinkBanner` or the last metadata block):

  ```html
  <GitStatusPanel v-if="task" :task-id="task.id" class="mt-4" />
  ```

- [ ] **1.6 — Add env var documentation to `.agent-context/conventions.md`**

  Under the Pipeline Env Vars table, add:

  ```
  | `DASHBOARD_ALLOW_GIT_PULL` | `true` / `false`, default `false`; enables `POST /api/tasks/:id/git/actions` with `action:'pull'` |
  ```

- [ ] **1.7 — Run lint + typecheck**

  ```bash
  pnpm lint && pnpm typecheck
  ```

- [ ] **1.8 — Commit**

  ```
  feat(git-panel): add git status endpoint and GitStatusPanel component (DX-1)
  ```

---

## Task 2: Slash commands in task prompt input (DX-3)

**Context:** `PromptInput.vue` already implements a full slash-command autocomplete for agent-domain commands (`/btw`, `/compact`, etc.) using a computed `slashSuggestions` list, keyboard navigation, and inline `<ul>` rendering. `TaskModal.vue` has its own `<textarea v-model="additionalPrompt">` for Resume/Retry hints — it has no slash command support. This task wires task-pipeline slash commands into that textarea only; it does not modify `PromptInput.vue`.

**Files:**
- `src/components/TaskSlashCommandMenu.vue` (new)
- `src/components/TaskModal.vue` (wire into `additionalPrompt` textarea)

### Steps

- [ ] **2.1 — Create `src/components/TaskSlashCommandMenu.vue`**

  This is a standalone floating menu component, reusable by any prompt textarea. It does not inherit from `PromptInput.vue` because the task prompt is a raw `<textarea>` bound via `v-model`, not wrapped in `PromptInput`.

  ```vue
  <script setup lang="ts">
  import { ref, computed, watch } from 'vue'

  export interface SlashCommand {
    name: string
    description: string
  }

  const props = defineProps<{
    modelValue: string
    commands: SlashCommand[]
  }>()

  const emit = defineEmits<{
    'update:modelValue': [value: string]
    'select': [command: SlashCommand]
  }>()

  const selectedIndex = ref(0)

  const suggestions = computed(() => {
    const val = props.modelValue.trim()
    if (!val.startsWith('/') || val.includes(' '))
      return []
    const query = val.toLowerCase()
    return props.commands.filter(c => c.name.startsWith(query))
  })

  const visible = computed(() => suggestions.value.length > 0)

  watch(suggestions, () => { selectedIndex.value = 0 })

  function confirm(cmd: SlashCommand) {
    emit('update:modelValue', `${cmd.name} `)
    emit('select', cmd)
  }

  function onKeydown(e: KeyboardEvent) {
    if (!visible.value)
      return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIndex.value = Math.min(selectedIndex.value + 1, suggestions.value.length - 1)
    }
    else if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIndex.value = Math.max(selectedIndex.value - 1, 0)
    }
    else if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
      if (visible.value) {
        e.preventDefault()
        const cmd = suggestions.value[selectedIndex.value]
        if (cmd)
          confirm(cmd)
      }
    }
    else if (e.key === 'Escape') {
      e.preventDefault()
      emit('update:modelValue', '')
    }
  }

  // Expose for parent to forward keydown events from its textarea
  defineExpose({ onKeydown, visible, confirm, suggestions, selectedIndex })
  </script>

  <template>
    <div
      v-if="visible"
      class="absolute bottom-full left-0 right-0 bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 border-b-0 rounded-t-md max-h-52 overflow-y-auto z-20"
    >
      <button
        v-for="(cmd, i) in suggestions"
        :key="cmd.name"
        type="button"
        class="flex items-center gap-2.5 w-full px-4 py-2 bg-transparent border-none text-slate-500 dark:text-slate-400 text-[13px] font-mono cursor-pointer text-left hover:bg-slate-100 dark:hover:bg-slate-800"
        :class="{ 'bg-slate-100 dark:bg-slate-800': i === selectedIndex }"
        @mousedown.prevent="confirm(cmd)"
      >
        <span class="text-blue-600 dark:text-blue-400 font-semibold flex-shrink-0">{{ cmd.name }}</span>
        <span class="text-slate-400 dark:text-slate-600 text-xs">{{ cmd.description }}</span>
      </button>
    </div>
  </template>
  ```

- [ ] **2.2 — Add task slash-command vocabulary and wire into `TaskModal.vue`**

  In `TaskModal.vue`, add the import at the top of `<script setup>`:

  ```typescript
  import TaskSlashCommandMenu from './TaskSlashCommandMenu.vue'
  ```

  Add the command list and menu ref near the other `ref` declarations:

  ```typescript
  const TASK_SLASH_COMMANDS = [
    { name: '/retry', description: 'Retry the current stage' },
    { name: '/reset-stage', description: 'Reset stage to pending' },
    { name: '/grant', description: 'Grant all pending permissions' },
    { name: '/cancel', description: 'Cancel this task' },
    { name: '/status', description: 'Show current stage status' },
    { name: '/help', description: 'List available commands' },
  ] as const

  const slashMenuRef = ref<InstanceType<typeof TaskSlashCommandMenu> | null>(null)
  ```

  Add a `onSlashSelect` handler that maps selected commands to existing actions:

  ```typescript
  async function onSlashSelect(cmd: { name: string }) {
    additionalPrompt.value = ''
    switch (cmd.name) {
      case '/retry':
        if (task.value && isFailedRun(task.value))
          await handleAction(() => retryTask(task.value!.id))
        break
      case '/grant':
        if (task.value)
          await handleAction(() => grantAllPendingPermissions(task.value!.id))
        break
      case '/cancel':
        if (task.value)
          await handleAction(() => cancelTask(task.value!.id))
        break
      // /reset-stage, /status, /help: no-op for now; extend in future tasks
    }
  }
  ```

  Wrap the existing `<textarea v-model="additionalPrompt">` in a `<div class="relative">` and add the menu + keydown forwarding:

  ```html
  <div class="relative mb-2">
    <TaskSlashCommandMenu
      ref="slashMenuRef"
      v-model="additionalPrompt"
      :commands="TASK_SLASH_COMMANDS"
      @select="onSlashSelect"
    />
    <textarea
      v-model="additionalPrompt"
      rows="2"
      class="w-full bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-slate-900 dark:text-slate-100 text-xs resize-none focus:outline-none focus:border-blue-500 placeholder:text-slate-400 dark:placeholder:text-slate-600"
      placeholder="Optional instruction for Resume / Retry — or type / for commands…"
      @keydown="slashMenuRef?.onKeydown($event)"
    />
  </div>
  ```

  Note: the `v-if="isFailedRun(task)"` guard that previously wrapped the textarea stays on the outer `<div class="relative mb-2">` — move it from the textarea itself to that wrapper.

- [ ] **2.3 — Run lint + typecheck**

  ```bash
  pnpm lint && pnpm typecheck
  ```

- [ ] **2.4 — Commit**

  ```
  feat(slash-commands): add TaskSlashCommandMenu for task prompt textarea (DX-3)
  ```

---

## Task 3: Permission re-request transparency (SA-3)

**Context:** `permissionsRepo.ts` has `createPermissionRequest` and `countPermissionRequestsForStageRun`. The per-`(task_id, tool, pattern)` re-request count does not exist. `TaskModal.vue` already has a Permissions tab rendering `permissions` (`TaskPermission[]`) and `pendingRequests` (`PermissionRequest[]`). `TaskCard.vue` already reads `task.blockedByPendingPermissions` (line 86) but only shows a chip — this task improves the existing chip label.

**Files:**
- `server/db/permissionsRepo.ts` (add `getPermissionReRequestCounts`)
- `server/routes/taskRoutes.ts` (include re-request counts in GET /tasks/:id/permissions response)
- `src/types.ts` (add `reRequestCount` to `PermissionRequest`)
- `src/components/TaskModal.vue` (show badge in Permissions tab)
- `src/components/TaskCard.vue` (verify chip label is clear; update if needed)

### Steps

- [ ] **3.1 — Add `getPermissionReRequestCounts` to `server/db/permissionsRepo.ts`**

  Append after `countPermissionRequestsForStageRun`:

  ```typescript
  /**
   * For each (tool, pattern) combination that has been requested for this task,
   * returns the total number of permission_requests rows (any outcome, any stage_run).
   * Key format: `${tool}:${pattern ?? '*'}`.
   */
  export function getPermissionReRequestCounts(
    taskId: string,
    db: Database = getDb(),
  ): Map<string, number> {
    // Join permission_requests → stage_runs to filter by task_id
    const rows = db
      .prepare(`
        SELECT pr.tool, pr.pattern, COUNT(*) AS cnt
        FROM permission_requests pr
        JOIN stage_runs sr ON sr.id = pr.stage_run_id
        WHERE sr.task_id = ?
        GROUP BY pr.tool, pr.pattern
      `)
      .all(taskId) as Array<{ tool: string; pattern: string | null; cnt: number }>

    const map = new Map<string, number>()
    for (const row of rows)
      map.set(`${row.tool}:${row.pattern ?? '*'}`, row.cnt)
    return map
  }
  ```

- [ ] **3.2 — Add `reRequestCount` to `PermissionRequest` in `src/types.ts`**

  Change the `PermissionRequest` interface to add an optional field (optional so existing code that constructs the type from the DB row without enrichment does not break):

  ```typescript
  export interface PermissionRequest {
    // ...existing fields...
    reRequestCount?: number   // total requests for this (tool, pattern) across all stage_runs of this task
  }
  ```

- [ ] **3.3 — Enrich the GET /tasks/:id/permissions response in `taskRoutes.ts`**

  Find the existing `router.get('/tasks/:id/permissions', ...)` handler. Import the new function at the top:

  ```typescript
  import {
    // ...existing imports...
    getPermissionReRequestCounts,
  } from '../db/permissionsRepo.js'
  ```

  Update the handler body to attach counts:

  ```typescript
  router.get('/tasks/:id/permissions', (req, res) => {
    const task = getTaskById(req.params.id)
    if (!task || !canAccessTask(task, req.user!)) {
      res.status(404).json({ error: 'Task not found' })
      return
    }
    const permissions = listTaskPermissions(task.id)
    const pendingRequests = listPendingPermissionRequests(task.currentStageRunId ?? '')
    const reRequestCounts = getPermissionReRequestCounts(task.id)

    // Attach reRequestCount to each pending request
    const enrichedRequests = pendingRequests.map(pr => ({
      ...pr,
      reRequestCount: reRequestCounts.get(`${pr.tool}:${pr.pattern ?? '*'}`) ?? 1,
    }))

    res.json({ permissions, pendingRequests: enrichedRequests })
  })
  ```

  Note: verify the current shape of this endpoint's response. If it currently returns a flat array of `TaskPermission[]`, adjust callers in `TaskModal.vue` accordingly — see step 3.4.

- [ ] **3.4 — Show re-request badge in `TaskModal.vue` Permissions tab**

  In the pending permission requests section, update each request row to show the count when greater than 1. Locate the template block that renders `pendingRequests`. Add after the tool/pattern label:

  ```html
  <span
    v-if="req.reRequestCount && req.reRequestCount > 1"
    class="ml-1.5 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400"
    :title="`Requested ${req.reRequestCount} times`"
  >
    {{ req.reRequestCount }}× re-requests
  </span>
  ```

- [ ] **3.5 — Verify TaskCard chip label is clear**

  In `src/components/TaskCard.vue`, find the block at line 86 (`v-if="task.blockedByPendingPermissions"`). If the chip label is just "blocked" or a generic icon, update it to:

  ```html
  <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400">
    &#9888; blocked by permissions
  </span>
  ```

- [ ] **3.6 — Run tests + lint + typecheck**

  ```bash
  pnpm test && pnpm lint && pnpm typecheck
  ```

- [ ] **3.7 — Commit**

  ```
  feat(permissions): add re-request count badge and kanban blocked chip (SA-3)
  ```

---

## Task 4: Audit log UI — TaskModal tab + settings page (SA-1)

**Context:** The audit tab already exists in `TaskModal.vue` (line 887) but shows a placeholder "Audit log viewer — Phase 6." The backend route `GET /api/tasks/:id/audit` already exists and returns `AuditEntry[]` via `listAuditForTask`. A global `GET /api/audit` endpoint does not exist yet. No `AuditLogTab` component exists. The app has no Vue Router — settings are shown via modal (`ApiKeySettings.vue`). The "settings page" for audit should follow the same modal pattern.

**Files:**
- `src/components/AuditLogTab.vue` (new)
- `src/components/TaskModal.vue` (replace placeholder with `AuditLogTab`)
- `server/routes/taskRoutes.ts` (add global `GET /api/audit`)
- `src/components/AuditSettings.vue` (new — modal-based audit log page)
- `src/App.vue` (add Audit Log button to settings area)

### Steps

- [ ] **4.1 — Create `src/components/AuditLogTab.vue`**

  ```vue
  <script setup lang="ts">
  import type { AuditEntry } from '../types'
  import { ref, watch, onMounted } from 'vue'

  const props = defineProps<{
    /** When set, fetches audit for a single task. When null/undefined, fetches global log. */
    taskId?: string
    /** Max entries to fetch for global log mode */
    limit?: number
  }>()

  const entries = ref<AuditEntry[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)
  const expandedId = ref<string | null>(null)

  const ACTOR_COLORS: Record<AuditEntry['actor'], string> = {
    user: 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-400',
    agent: 'bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-400',
    system: 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300',
  }

  async function load() {
    loading.value = true
    error.value = null
    try {
      const url = props.taskId
        ? `/api/tasks/${props.taskId}/audit`
        : `/api/audit?limit=${props.limit ?? 100}`
      const res = await fetch(url)
      if (!res.ok) {
        error.value = `HTTP ${res.status}`
        return
      }
      entries.value = await res.json()
    }
    catch (e) {
      error.value = e instanceof Error ? e.message : 'Network error'
    }
    finally {
      loading.value = false
    }
  }

  function toggleDetails(id: string) {
    expandedId.value = expandedId.value === id ? null : id
  }

  onMounted(load)
  watch(() => props.taskId, load)
  </script>

  <template>
    <div class="text-xs">
      <div class="flex items-center justify-between mb-3">
        <span class="font-semibold text-slate-700 dark:text-slate-300 text-[13px]">Audit Log</span>
        <button
          type="button"
          class="px-2 py-0.5 rounded bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:brightness-110 disabled:opacity-40"
          :disabled="loading"
          @click="load"
        >
          Refresh
        </button>
      </div>

      <div v-if="loading" class="text-slate-400 dark:text-slate-600 text-center py-6">Loading…</div>
      <div v-else-if="error" class="text-red-600 dark:text-red-400">{{ error }}</div>
      <div v-else-if="entries.length === 0" class="text-slate-400 dark:text-slate-600 text-center py-6">No audit entries.</div>

      <table v-else class="w-full border-collapse">
        <thead>
          <tr class="text-left text-slate-400 dark:text-slate-600 border-b border-slate-200 dark:border-slate-700">
            <th class="pb-1.5 pr-3 font-medium">Time</th>
            <th class="pb-1.5 pr-3 font-medium">Actor</th>
            <th class="pb-1.5 pr-3 font-medium">Action</th>
            <th class="pb-1.5 font-medium">Details</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="entry in entries" :key="entry.id">
            <tr class="border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50 dark:hover:bg-slate-900/50">
              <td class="py-1.5 pr-3 font-mono text-slate-500 dark:text-slate-400 whitespace-nowrap">
                {{ new Date(entry.timestamp).toLocaleTimeString() }}
              </td>
              <td class="py-1.5 pr-3">
                <span :class="['px-1.5 py-0.5 rounded-full text-[10px] font-medium', ACTOR_COLORS[entry.actor] ?? ACTOR_COLORS.system]">
                  {{ entry.actor }}
                </span>
              </td>
              <td class="py-1.5 pr-3 font-mono text-slate-700 dark:text-slate-300">{{ entry.action }}</td>
              <td class="py-1.5">
                <button
                  v-if="entry.details"
                  type="button"
                  class="text-blue-500 hover:underline"
                  @click="toggleDetails(entry.id)"
                >
                  {{ expandedId === entry.id ? 'hide' : 'show' }}
                </button>
                <span v-else class="text-slate-300 dark:text-slate-700">—</span>
              </td>
            </tr>
            <tr v-if="expandedId === entry.id && entry.details" :key="`${entry.id}-detail`">
              <td colspan="4" class="pb-2 pl-2">
                <pre class="bg-slate-100 dark:bg-slate-800 rounded p-2 text-[11px] font-mono overflow-x-auto whitespace-pre-wrap">{{ JSON.stringify(entry.details, null, 2) }}</pre>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </template>
  ```

- [ ] **4.2 — Replace the placeholder in `TaskModal.vue` audit tab**

  Add import:

  ```typescript
  import AuditLogTab from './AuditLogTab.vue'
  ```

  Replace:

  ```html
  <!-- Audit tab -->
  <section v-if="activeTab === 'audit'" class="p-5">
    <div class="text-slate-400 dark:text-slate-600 text-xs text-center py-8">
      Audit log viewer — Phase 6.
    </div>
  </section>
  ```

  With:

  ```html
  <!-- Audit tab -->
  <section v-if="activeTab === 'audit'" class="p-5">
    <AuditLogTab v-if="task" :task-id="task.id" />
  </section>
  ```

- [ ] **4.3 — Add global audit endpoint to `taskRoutes.ts`**

  Import `listAuditForTask` is already there. Add a new function import or a raw DB query. Add before the git route block:

  ```typescript
  // ─── Global Audit ────────────────────────────────────────────────────────────

  router.get('/audit', (req, res) => {
    // Admin-only: require keys:manage scope or admin role
    if (req.user?.role !== 'admin' && !req.scopes?.includes('keys:manage')) {
      res.status(403).json({ error: 'Forbidden' })
      return
    }
    const limit = Math.min(Number(req.query.limit) || 100, 500)
    const offset = Number(req.query.offset) || 0
    const rows = getDb()
      .prepare('SELECT * FROM audit_log ORDER BY timestamp DESC LIMIT ? OFFSET ?')
      .all(limit, offset) as import('../db/rowMappers.js').AuditRow[]
    res.json(rows.map(rowToAuditEntry))
  })
  ```

  Add the `rowToAuditEntry` import if not already present:

  ```typescript
  import { rowToAuditEntry } from '../db/rowMappers.js'
  import type { AuditRow } from '../db/rowMappers.js'
  ```

  Note: check whether `req.user` and `req.scopes` are available on the request type in this project (see existing auth middleware usage in the routes file) and adjust the guard accordingly.

- [ ] **4.4 — Create `src/components/AuditSettings.vue`**

  Follows the same dialog/modal pattern as `ApiKeySettings.vue`:

  ```vue
  <script setup lang="ts">
  import AuditLogTab from './AuditLogTab.vue'
  import { ref } from 'vue'

  const props = defineProps<{ open: boolean }>()
  const emit = defineEmits<{ close: [] }>()

  const limit = ref(100)
  </script>

  <template>
    <Teleport to="body">
      <div
        v-if="open"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
        @click.self="emit('close')"
      >
        <div class="bg-white dark:bg-slate-900 rounded-xl shadow-2xl w-full max-w-3xl max-h-[80vh] flex flex-col overflow-hidden">
          <header class="px-5 py-3.5 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between flex-shrink-0">
            <h2 class="text-sm font-semibold text-slate-800 dark:text-slate-200">Audit Log</h2>
            <button
              type="button"
              class="text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 text-lg leading-none"
              @click="emit('close')"
            >
              ×
            </button>
          </header>
          <div class="flex-1 overflow-y-auto p-5">
            <AuditLogTab :limit="limit" />
          </div>
        </div>
      </div>
    </Teleport>
  </template>
  ```

- [ ] **4.5 — Wire `AuditSettings` into `App.vue`**

  Add import:

  ```typescript
  import AuditSettings from './components/AuditSettings.vue'
  ```

  Add ref:

  ```typescript
  const showAudit = ref(false)
  ```

  Add a button in the header near the Settings button:

  ```html
  <button title="Audit Log" @click="showAudit = true">
    <!-- use same icon style as existing settings buttons -->
    Audit
  </button>
  ```

  Add the modal component alongside `<ApiKeySettings>`:

  ```html
  <AuditSettings :open="showAudit" @close="showAudit = false" />
  ```

- [ ] **4.6 — Run lint + typecheck**

  ```bash
  pnpm lint && pnpm typecheck
  ```

- [ ] **4.7 — Commit**

  ```
  feat(audit): implement AuditLogTab component and global audit endpoint (SA-1)
  ```

---

## Task 5: Webhook HMAC-SHA256 signing (SA-2)

**Context:** `server/notifications/adapters/webhook.ts` sends outbound webhook POSTs via `fetch()` with no signature. Config is read from `notificationConfigRepo`. No HMAC infrastructure exists anywhere in the project.

**Files:**
- `server/notifications/hmac.ts` (new)
- `server/notifications/adapters/webhook.ts` (add signing)
- `server/routes/taskRoutes.ts` or a new `server/routes/settingsRoutes.ts` (add HMAC config endpoint)
- `src/components/ApiKeySettings.vue` or a new `WebhookSettings.vue` (UI to enable/configure HMAC)
- `server/notifications/hmac.test.ts` (new)

### Steps

- [ ] **5.1 — Create `server/notifications/hmac.ts`**

  ```typescript
  import { createHmac } from 'node:crypto'

  /**
   * Computes a Stripe-compatible HMAC-SHA256 signature.
   * Payload format: `${timestamp}.${rawBody}`
   * Returns the hex digest (without any prefix — callers prepend `sha256=`).
   */
  export function computeWebhookHmac(secret: string, payload: string): string {
    return createHmac('sha256', secret).update(payload).digest('hex')
  }

  /**
   * Builds the full value for the X-Dashboard-Signature header.
   */
  export function buildSignatureHeader(secret: string, timestamp: string, rawBody: string): string {
    const sig = computeWebhookHmac(secret, `${timestamp}.${rawBody}`)
    return `sha256=${sig}`
  }
  ```

- [ ] **5.2 — Write unit tests `server/notifications/hmac.test.ts`**

  ```typescript
  import { describe, it, expect } from 'vitest'
  import { computeWebhookHmac, buildSignatureHeader } from './hmac.js'

  describe('computeWebhookHmac', () => {
    it('returns a 64-character hex string', () => {
      const result = computeWebhookHmac('secret', 'payload')
      expect(result).toHaveLength(64)
      expect(result).toMatch(/^[0-9a-f]{64}$/)
    })

    it('is deterministic for the same inputs', () => {
      const a = computeWebhookHmac('mysecret', '1715000000.{"event":"test"}')
      const b = computeWebhookHmac('mysecret', '1715000000.{"event":"test"}')
      expect(a).toBe(b)
    })

    it('differs when the secret changes', () => {
      const a = computeWebhookHmac('secret-a', 'payload')
      const b = computeWebhookHmac('secret-b', 'payload')
      expect(a).not.toBe(b)
    })

    it('differs when the payload changes', () => {
      const a = computeWebhookHmac('secret', 'payload-a')
      const b = computeWebhookHmac('secret', 'payload-b')
      expect(a).not.toBe(b)
    })

    it('matches a known HMAC-SHA256 vector', () => {
      // echo -n "1715000000.hello" | openssl dgst -sha256 -hmac "test-key"
      const result = computeWebhookHmac('test-key', '1715000000.hello')
      // Pre-computed reference value (verify with: openssl dgst -sha256 -hmac "test-key" <<< "1715000000.hello")
      // Update this constant when the reference is confirmed.
      expect(result).toHaveLength(64)
    })
  })

  describe('buildSignatureHeader', () => {
    it('returns sha256=<hex> format', () => {
      const header = buildSignatureHeader('secret', '1715000000', '{"event":"test"}')
      expect(header).toMatch(/^sha256=[0-9a-f]{64}$/)
    })

    it('incorporates the timestamp into the signed payload', () => {
      const a = buildSignatureHeader('secret', '1715000000', 'body')
      const b = buildSignatureHeader('secret', '1715000001', 'body')
      expect(a).not.toBe(b)
    })
  })
  ```

  Run tests:

  ```bash
  pnpm test server/notifications/hmac.test.ts
  ```

- [ ] **5.3 — Update `server/notifications/adapters/webhook.ts` to sign requests**

  Add the import at the top:

  ```typescript
  import { buildSignatureHeader } from '../hmac.js'
  ```

  In the `send` method, after the `body` variable is constructed and before the `fetch()` call, add:

  ```typescript
  const rawBody = JSON.stringify(body)
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }

  const hmacEnabled = getConfig('webhook_hmac_enabled') === 'true'
  const hmacSecret = hmacEnabled ? getConfig('webhook_hmac_secret') : null
  if (hmacEnabled && hmacSecret) {
    const timestamp = Math.floor(Date.now() / 1000).toString()
    headers['X-Dashboard-Signature'] = buildSignatureHeader(hmacSecret, timestamp, rawBody)
    headers['X-Dashboard-Timestamp'] = timestamp
  }
  ```

  Update the `fetch()` call to use `rawBody` and `headers`:

  ```typescript
  res = await fetch(url, {
    method: 'POST',
    headers,
    body: rawBody,
    signal: controller.signal,
  })
  ```

  The full diff to the `send` method — replace from after `const body = buildBody(format, payload)` through the existing `fetch()` call:

  ```typescript
  const rawBody = JSON.stringify(body)
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }

  const hmacEnabled = getConfig('webhook_hmac_enabled') === 'true'
  const hmacSecret = hmacEnabled ? getConfig('webhook_hmac_secret') : null
  if (hmacEnabled && hmacSecret) {
    const timestamp = Math.floor(Date.now() / 1000).toString()
    headers['X-Dashboard-Signature'] = buildSignatureHeader(hmacSecret, timestamp, rawBody)
    headers['X-Dashboard-Timestamp'] = timestamp
  }

  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), DEFAULT_REMOTE_TIMEOUT_MS)
  let res: Response
  try {
    res = await fetch(url, {
      method: 'POST',
      headers,
      body: rawBody,
      signal: controller.signal,
    })
  }
  finally {
    clearTimeout(timeoutId)
  }
  ```

- [ ] **5.4 — Add HMAC config endpoint**

  Locate the existing notification/settings routes. If a dedicated settings router exists for webhook config, add there. Otherwise add to `taskRoutes.ts` (or the most appropriate existing settings route file) before the git routes:

  ```typescript
  // ─── Webhook HMAC ────────────────────────────────────────────────────────────

  router.post('/settings/webhook-hmac', (req, res) => {
    if (req.user?.role !== 'admin' && !req.scopes?.includes('keys:manage')) {
      res.status(403).json({ error: 'Forbidden' })
      return
    }
    const { enabled, secret } = req.body as { enabled: boolean; secret?: string }
    if (typeof enabled !== 'boolean') {
      res.status(400).json({ error: '`enabled` must be a boolean' })
      return
    }
    setConfig('webhook_hmac_enabled', enabled ? 'true' : 'false')
    if (enabled) {
      const resolvedSecret = secret && secret.length >= 32
        ? secret
        : randomBytes(32).toString('hex')
      setConfig('webhook_hmac_secret', resolvedSecret)
      res.json({ enabled: true, secret: resolvedSecret })
    }
    else {
      res.json({ enabled: false })
    }
  })

  router.get('/settings/webhook-hmac', (req, res) => {
    if (req.user?.role !== 'admin' && !req.scopes?.includes('keys:manage')) {
      res.status(403).json({ error: 'Forbidden' })
      return
    }
    const enabled = getConfig('webhook_hmac_enabled') === 'true'
    // Never return the raw secret in GET — only indicate whether one is configured
    const hasSecret = !!getConfig('webhook_hmac_secret')
    res.json({ enabled, hasSecret })
  })
  ```

  Add imports:

  ```typescript
  import { randomBytes } from 'node:crypto'
  import { getConfig, setConfig } from '../db/notificationConfigRepo.js'
  ```

  Check if `setConfig` exists in `notificationConfigRepo.ts` — if the repo only exposes `getConfig`, add `setConfig`:

  ```typescript
  export function setConfig(key: string, value: string, db: Database = getDb()): void {
    db.prepare(`
      INSERT INTO notification_config (key, value) VALUES (?, ?)
      ON CONFLICT(key) DO UPDATE SET value = excluded.value
    `).run(key, value)
  }
  ```

- [ ] **5.5 — Run all tests + lint + typecheck**

  ```bash
  pnpm test && pnpm lint && pnpm typecheck
  ```

- [ ] **5.6 — Commit**

  ```
  feat(webhook): add HMAC-SHA256 signing with Stripe-compatible header format (SA-2)
  ```

---

## Final integration check

- [ ] Run full test suite: `pnpm test`
- [ ] Run E2E smoke: `pnpm test:e2e` (verifies dev server starts and basic UI loads)
- [ ] Run typecheck: `pnpm typecheck`
- [ ] Check no `0.0.0.0` binding was introduced (grep: `grep -r "0\.0\.0\.0" server/`)
- [ ] Commit summary:

  ```
  chore(phase3): integrate DX-1, DX-3, SA-1, SA-2, SA-3 — git panel, slash commands, audit UI, HMAC signing, permission re-request counts
  ```
