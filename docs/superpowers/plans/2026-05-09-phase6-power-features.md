# Phase 6 — Power Features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Global search (Cmd+K), epic grouping, memory file browser, N-gram pattern discovery, per-agent identity, Python statusline, quota tracking, PWA.

**Architecture:** Search uses SQLite FTS5 virtual table over task titles + a lightweight in-memory agent index. Epic grouping is frontend-only (group by parentTaskId). Memory browser is a thin REST wrapper over ~/.claude filesystem reads/writes.

**Tech Stack:** SQLite FTS5, Vue 3, Python 3, Vite PWA plugin

---

## Task 1: Cross-Session Full-Text Search — SQLite FTS5 (IP-4)

**Files:**
- `server/db/client.ts` — migration: create `task_fts` FTS5 table + sync triggers
- `server/routes/searchRoutes.ts` — new `GET /api/search` endpoint
- `server/index.ts` — register search router

### Steps

- [ ] **1.1** Add FTS5 migration to `server/db/client.ts`

  At the end of the migration block (after the existing table setup), add:

  ```typescript
  // FTS5 full-text search over task titles and descriptions
  db.exec(`
    CREATE VIRTUAL TABLE IF NOT EXISTS task_fts
    USING fts5(
      task_id UNINDEXED,
      title,
      description,
      content='tasks',
      content_rowid='rowid'
    )
  `)

  // Keep FTS index in sync with tasks table
  db.exec(`
    CREATE TRIGGER IF NOT EXISTS tasks_fts_insert
    AFTER INSERT ON tasks BEGIN
      INSERT INTO task_fts(rowid, task_id, title, description)
      VALUES (new.rowid, new.id, new.title, COALESCE(new.description, ''));
    END
  `)
  db.exec(`
    CREATE TRIGGER IF NOT EXISTS tasks_fts_update
    AFTER UPDATE ON tasks BEGIN
      INSERT INTO task_fts(task_fts, rowid, task_id, title, description)
      VALUES ('delete', old.rowid, old.id, old.title, COALESCE(old.description, ''));
      INSERT INTO task_fts(rowid, task_id, title, description)
      VALUES (new.rowid, new.id, new.title, COALESCE(new.description, ''));
    END
  `)
  db.exec(`
    CREATE TRIGGER IF NOT EXISTS tasks_fts_delete
    AFTER DELETE ON tasks BEGIN
      INSERT INTO task_fts(task_fts, rowid, task_id, title, description)
      VALUES ('delete', old.rowid, old.id, old.title, COALESCE(old.description, ''));
    END
  `)
  ```

  Backfill existing tasks (runs once per process start, idempotent via INSERT OR IGNORE):

  ```typescript
  db.exec(`
    INSERT OR IGNORE INTO task_fts(rowid, task_id, title, description)
    SELECT rowid, id, title, COALESCE(description, '') FROM tasks
  `)
  ```

- [ ] **1.2** Create `server/routes/searchRoutes.ts`

  ```typescript
  import type { Agent } from '../../src/types.js'
  import { Router } from 'express'
  import { getDb } from '../db/client.js'
  import { getTaskById } from '../db/tasksRepo.js'

  interface SearchDeps {
    getAgents: () => Agent[]
  }

  export function createSearchRouter({ getAgents }: SearchDeps): ReturnType<typeof Router> {
    const router = Router()

    router.get('/search', (req, res) => {
      const q = ((req.query.q as string) ?? '').trim()
      const type = (req.query.type as string) ?? 'all'
      const limit = Math.min(50, Number(req.query.limit ?? 20))

      if (!q) {
        res.json({ tasks: [], agents: [] })
        return
      }

      const db = getDb()

      // FTS5 task search
      let tasks: ReturnType<typeof getTaskById>[] = []
      if (type === 'tasks' || type === 'all') {
        try {
          const rows = db.prepare(`
            SELECT task_id FROM task_fts
            WHERE task_fts MATCH ?
            ORDER BY rank
            LIMIT ?
          `).all(`${q}*`, limit) as Array<{ task_id: string }>
          tasks = rows.map(r => getTaskById(r.task_id)).filter(Boolean)
        }
        catch {
          // FTS match syntax error — return empty rather than 500
        }
      }

      // In-memory agent search
      let agents: Agent[] = []
      if (type === 'agents' || type === 'all') {
        const ql = q.toLowerCase()
        agents = getAgents()
          .filter(a =>
            a.projectName.toLowerCase().includes(ql)
            || (a.currentAction ?? '').toLowerCase().includes(ql)
            || a.cwd.toLowerCase().includes(ql),
          )
          .slice(0, limit)
      }

      res.json({ tasks, agents })
    })

    return router
  }
  ```

- [ ] **1.3** Register search router in `server/index.ts`

  ```typescript
  import { createSearchRouter } from './routes/searchRoutes.js'

  // After agentRouter is registered:
  let cachedAgents: Agent[] = []
  // Update cachedAgents in the SSE broadcast loop (already runs periodically)
  // Pass a getter into the router:
  app.use('/api', createSearchRouter({ getAgents: () => cachedAgents }))
  ```

  In the SSE broadcast that already calls `getAgents()`, assign: `cachedAgents = agents`.

- [ ] **1.4** Write Vitest test `server/routes/searchRoutes.test.ts`

  ```typescript
  import { describe, expect, it } from 'vitest'
  import express from 'express'
  import request from 'supertest'
  import { createSearchRouter } from './searchRoutes.js'

  function makeApp() {
    const app = express()
    app.use(express.json())
    app.use('/api', createSearchRouter({ getAgents: () => [] }))
    return app
  }

  describe('searchRoutes', () => {
    it('returns empty results for blank query', async () => {
      const app = makeApp()
      const res = await request(app).get('/api/search?q=')
      expect(res.status).toBe(200)
      expect(res.body.tasks).toEqual([])
      expect(res.body.agents).toEqual([])
    })

    it('does not throw on FTS syntax error query', async () => {
      const app = makeApp()
      const res = await request(app).get('/api/search?q=AND')
      expect(res.status).toBe(200)
    })
  })
  ```

- [ ] **1.5** Run

  ```bash
  pnpm test --run server/routes/searchRoutes.test.ts
  pnpm typecheck
  ```

- [ ] **1.6** Commit

  ```
  feat(search): add SQLite FTS5 full-text search index and /api/search endpoint (IP-4)
  ```

---

## Task 2: Spotlight / Cmd+K Global Search (DX-5)

**Files:**
- `src/components/SpotlightSearch.vue` — modal overlay with keyboard navigation
- `src/App.vue` — mount globally, bind Cmd+K

### Steps

- [ ] **2.1** Create `src/components/SpotlightSearch.vue`

  ```vue
  <script setup lang="ts">
  import type { Agent, PipelineTask } from '../types'
  import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
  import AppModal from './ui/AppModal.vue'

  const emit = defineEmits<{
    navigateTask: [task: PipelineTask]
    navigateAgent: [agent: Agent]
  }>()

  const open = ref(false)
  const query = ref('')
  const inputRef = ref<HTMLInputElement | null>(null)
  const selectedIdx = ref(0)

  interface SearchResults {
    tasks: PipelineTask[]
    agents: Agent[]
  }

  const results = ref<SearchResults>({ tasks: [], agents: [] })
  const loading = ref(false)
  let debounceHandle: ReturnType<typeof setTimeout> | null = null

  const flatResults = computed((): Array<{ type: 'task', item: PipelineTask } | { type: 'agent', item: Agent }> => {
    return [
      ...results.value.tasks.map(t => ({ type: 'task' as const, item: t })),
      ...results.value.agents.map(a => ({ type: 'agent' as const, item: a })),
    ]
  })

  async function search(q: string) {
    if (!q.trim()) {
      results.value = { tasks: [], agents: [] }
      return
    }
    loading.value = true
    try {
      const res = await fetch(`/api/search?q=${encodeURIComponent(q)}&type=all&limit=10`)
      results.value = await res.json() as SearchResults
      selectedIdx.value = 0
    }
    finally {
      loading.value = false
    }
  }

  watch(query, (q) => {
    if (debounceHandle)
      clearTimeout(debounceHandle)
    debounceHandle = setTimeout(() => search(q), 200)
  })

  function onKeydown(e: KeyboardEvent) {
    // Open on Cmd+K or Ctrl+K
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault()
      open.value = !open.value
      if (open.value)
        nextTick(() => inputRef.value?.focus())
      return
    }
    if (!open.value)
      return
    if (e.key === 'Escape') {
      open.value = false
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIdx.value = Math.min(selectedIdx.value + 1, flatResults.value.length - 1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIdx.value = Math.max(selectedIdx.value - 1, 0)
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      const selected = flatResults.value[selectedIdx.value]
      if (!selected)
        return
      if (selected.type === 'task')
        emit('navigateTask', selected.item)
      else
        emit('navigateAgent', selected.item)
      open.value = false
    }
  }

  onMounted(() => window.addEventListener('keydown', onKeydown))
  onUnmounted(() => window.removeEventListener('keydown', onKeydown))
  </script>

  <template>
    <AppModal :open="open" :z-index="2000" @close="open = false">
      <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-2xl w-full max-w-lg overflow-hidden">
        <div class="flex items-center gap-2 px-4 py-3 border-b border-slate-200 dark:border-slate-700">
          <span class="text-slate-400 text-sm">⌘K</span>
          <input
            ref="inputRef"
            v-model="query"
            type="text"
            placeholder="Search tasks and agents…"
            class="flex-1 bg-transparent text-sm text-slate-900 dark:text-slate-100 outline-none placeholder:text-slate-400"
          >
          <span v-if="loading" class="text-xs text-slate-400">Searching…</span>
        </div>
        <div class="max-h-80 overflow-y-auto">
          <template v-if="flatResults.length === 0 && query">
            <p class="px-4 py-3 text-sm text-slate-400">No results for "{{ query }}"</p>
          </template>
          <template v-else>
            <div v-if="results.tasks.length > 0" class="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
              Tasks
            </div>
            <button
              v-for="(item, idx) in flatResults"
              :key="`${item.type}-${item.type === 'task' ? item.item.id : (item.item as Agent).sessionId}`"
              type="button"
              :class="[
                'w-full text-left px-4 py-2 text-sm flex items-center gap-3 transition-colors',
                selectedIdx === idx
                  ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300'
                  : 'text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800',
              ]"
              @click="() => {
                if (item.type === 'task') { emit('navigateTask', item.item); open = false }
                else { emit('navigateAgent', item.item as Agent); open = false }
              }"
              @mouseenter="selectedIdx = idx"
            >
              <span class="text-[10px] uppercase tracking-wide text-slate-400 w-10 flex-shrink-0">
                {{ item.type === 'task' ? 'Task' : 'Agent' }}
              </span>
              <span class="truncate">
                {{ item.type === 'task' ? (item.item as PipelineTask).title : (item.item as Agent).projectName }}
              </span>
              <span class="ml-auto text-[10px] text-slate-400">
                {{ item.type === 'task' ? (item.item as PipelineTask).currentStage : (item.item as Agent).status }}
              </span>
            </button>
            <div v-if="results.agents.length > 0 && results.tasks.length > 0" class="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-slate-400 border-t border-slate-100 dark:border-slate-800 mt-1">
              Agents
            </div>
          </template>
        </div>
        <div class="px-4 py-2 border-t border-slate-100 dark:border-slate-800 flex gap-3 text-[10px] text-slate-400">
          <span>↑↓ navigate</span>
          <span>↵ open</span>
          <span>Esc close</span>
        </div>
      </div>
    </AppModal>
  </template>
  ```

- [ ] **2.2** Add `<SpotlightSearch>` to `src/App.vue`

  Import:

  ```typescript
  import SpotlightSearch from './components/SpotlightSearch.vue'
  ```

  In template (outside all modals, at the root level):

  ```html
  <SpotlightSearch
    @navigate-task="task => { selectedTask = task; showTaskModal = true }"
    @navigate-agent="agent => { selectedAgent = agent; showAgentModal = true }"
  />
  ```

  Adjust event handler prop names to match the existing modal state variables in `App.vue`.

- [ ] **2.3** Write component test `src/components/SpotlightSearch.test.ts`

  ```typescript
  import { describe, expect, it, vi } from 'vitest'
  import { mount } from '@vue/test-utils'
  import SpotlightSearch from './SpotlightSearch.vue'

  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ tasks: [], agents: [] }),
  }))

  describe('SpotlightSearch', () => {
    it('is hidden by default', () => {
      const wrapper = mount(SpotlightSearch)
      expect(wrapper.find('input').exists()).toBe(false)
    })

    it('opens on Cmd+K', async () => {
      const wrapper = mount(SpotlightSearch)
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
      await wrapper.vm.$nextTick()
      expect(wrapper.find('input').exists()).toBe(true)
    })
  })
  ```

- [ ] **2.4** Run

  ```bash
  pnpm test --run src/components/SpotlightSearch.test.ts
  pnpm typecheck
  ```

- [ ] **2.5** Commit

  ```
  feat(ux): add Cmd+K Spotlight global search modal (DX-5)
  ```

---

## Task 3: Epic Grouping with Completion % (VA-4)

**Files:**
- `src/components/PipelineBoard.vue` — refactor to group by parentTaskId

### Steps

- [ ] **3.1** Modify `src/components/PipelineBoard.vue`

  In `<script setup>`, add:

  ```typescript
  import { computed, ref } from 'vue'
  import { useTasks } from '../composables/useTasks'

  const { tasks } = useTasks()

  interface Epic {
    parent: PipelineTask
    children: PipelineTask[]
    doneCount: number
    totalCount: number
    completionPct: number
  }

  const epics = computed((): Epic[] => {
    const parentIds = new Set(tasks.value.filter(t => t.parentTaskId).map(t => t.parentTaskId!))
    return [...parentIds].map((parentId) => {
      const parent = tasks.value.find(t => t.id === parentId)
      if (!parent)
        return null
      const children = tasks.value.filter(t => t.parentTaskId === parentId)
      const doneCount = children.filter(c => c.currentStage === 'done').length
      return {
        parent,
        children,
        doneCount,
        totalCount: children.length,
        completionPct: children.length > 0 ? Math.round((doneCount / children.length) * 100) : 0,
      }
    }).filter(Boolean) as Epic[]
  })

  const epicExpanded = ref<Record<string, boolean>>({})
  function toggleEpic(id: string) {
    epicExpanded.value[id] = !epicExpanded.value[id]
  }
  ```

  In the template, add epic headers above the task cards in each column. The SVG progress ring uses `r=9`, circumference = 2π×9 ≈ 56.55:

  ```html
  <!-- Epic headers with progress rings, shown in columns where they have children -->
  <template v-for="epic in epics" :key="epic.parent.id">
    <div
      v-if="tasksForColumn(col).some(t => t.parentTaskId === epic.parent.id)"
      class="mb-2 border border-blue-200 dark:border-blue-800 rounded-lg overflow-hidden"
    >
      <button
        type="button"
        class="w-full flex items-center gap-2 px-3 py-2 bg-blue-50 dark:bg-blue-950 text-left"
        @click="toggleEpic(epic.parent.id)"
      >
        <svg width="24" height="24" viewBox="0 0 24 24" class="flex-shrink-0">
          <circle cx="12" cy="12" r="9" fill="none" stroke="#e2e8f0" stroke-width="3" />
          <circle
            cx="12" cy="12" r="9" fill="none"
            stroke="#3b82f6" stroke-width="3"
            stroke-dasharray="56.55"
            :stroke-dashoffset="56.55 * (1 - epic.completionPct / 100)"
            stroke-linecap="round"
            transform="rotate(-90 12 12)"
          />
        </svg>
        <span class="text-xs font-semibold text-slate-700 dark:text-slate-200 truncate flex-1">{{ epic.parent.title }}</span>
        <span class="text-[10px] text-slate-400 flex-shrink-0">{{ epic.doneCount }}/{{ epic.totalCount }} ({{ epic.completionPct }}%)</span>
        <span class="text-xs text-slate-400">{{ epicExpanded[epic.parent.id] ? '▲' : '▼' }}</span>
      </button>
      <div v-if="epicExpanded[epic.parent.id]" class="pl-3 pr-2 pb-2 pt-1 space-y-1.5">
        <TaskCard
          v-for="child in epic.children.filter(c => tasksForColumn(col).some(t => t.id === c.id))"
          :key="child.id"
          :task="child"
          @click="emit('select', child)"
        />
      </div>
    </div>
  </template>
  ```

- [ ] **3.2** Run typecheck

  ```bash
  pnpm typecheck
  pnpm lint
  ```

- [ ] **3.3** Commit

  ```
  feat(ux): add epic grouping with SVG progress ring in kanban board (VA-4)
  ```

---

## Task 4: Memory File Browser (DX-6)

**Files:**
- `server/routes/memoryRoutes.ts` — REST list/read/write with path validation
- `server/index.ts` — register router
- `src/components/MemoryBrowser.vue` — file tree + editor

### Steps

- [ ] **4.1** Create `server/routes/memoryRoutes.ts`

  ```typescript
  import { readdir, readFile, writeFile } from 'node:fs/promises'
  import { join, resolve } from 'node:path'
  import { homedir } from 'node:os'
  import { Router } from 'express'

  const CLAUDE_ROOT = resolve(join(homedir(), '.claude'))

  /**
   * Resolve an encoded path segment relative to CLAUDE_ROOT.
   * Returns null if the resolved path escapes the CLAUDE_ROOT subtree
   * (TOCTOU-safe: resolution happens before any I/O).
   */
  function safePath(encoded: string): string | null {
    const decoded = decodeURIComponent(encoded)
    // resolve() normalizes all .. segments deterministically
    const resolved = resolve(CLAUDE_ROOT, decoded)
    if (!resolved.startsWith(CLAUDE_ROOT + '/') && resolved !== CLAUDE_ROOT)
      return null
    return resolved
  }

  export function createMemoryRouter(): ReturnType<typeof Router> {
    const router = Router()

    // List memory files across all project memory dirs
    router.get('/memory', async (_req, res) => {
      try {
        const projectsDir = join(CLAUDE_ROOT, 'projects')
        const projects = await readdir(projectsDir).catch(() => [])
        const files: Array<{ path: string, name: string }> = []

        for (const project of projects) {
          const memDir = join(projectsDir, project, 'memory')
          const entries = await readdir(memDir).catch(() => [])
          for (const entry of entries) {
            if (entry.endsWith('.md')) {
              const relPath = join('projects', project, 'memory', entry)
              files.push({ path: relPath, name: `${project}/${entry}` })
            }
          }
        }
        res.json({ files })
      }
      catch {
        res.status(500).json({ error: 'Failed to list memory files' })
      }
    })

    // Read a single memory file
    router.get('/memory/:encoded(*)', async (req, res) => {
      const safe = safePath(req.params.encoded)
      if (!safe) {
        res.status(400).json({ error: 'Path traversal detected' })
        return
      }
      try {
        const content = await readFile(safe, 'utf8')
        res.json({ content })
      }
      catch {
        res.status(404).json({ error: 'File not found' })
      }
    })

    // Write a memory file (restricted to ~/.claude subtree)
    router.put('/memory/:encoded(*)', async (req, res) => {
      const safe = safePath(req.params.encoded)
      if (!safe) {
        res.status(400).json({ error: 'Path traversal detected' })
        return
      }
      const { content } = req.body as { content: string }
      if (typeof content !== 'string') {
        res.status(400).json({ error: 'content must be a string' })
        return
      }
      try {
        await writeFile(safe, content, 'utf8')
        res.json({ ok: true })
      }
      catch {
        res.status(500).json({ error: 'Failed to write file' })
      }
    })

    return router
  }
  ```

- [ ] **4.2** Register in `server/index.ts`:

  ```typescript
  import { createMemoryRouter } from './routes/memoryRoutes.js'
  app.use('/api', createMemoryRouter())
  ```

- [ ] **4.3** Create `src/components/MemoryBrowser.vue`

  ```vue
  <script setup lang="ts">
  import { onMounted, ref } from 'vue'
  import AppButton from './ui/AppButton.vue'

  interface MemoryFile {
    path: string
    name: string
  }

  const files = ref<MemoryFile[]>([])
  const selectedPath = ref<string | null>(null)
  const content = ref('')
  const saving = ref(false)
  const saved = ref(false)

  async function loadFiles() {
    const res = await fetch('/api/memory')
    if (res.ok) {
      const data = await res.json() as { files: MemoryFile[] }
      files.value = data.files
    }
  }

  async function openFile(path: string) {
    selectedPath.value = path
    const res = await fetch(`/api/memory/${encodeURIComponent(path)}`)
    if (res.ok) {
      const data = await res.json() as { content: string }
      content.value = data.content
    }
  }

  async function save() {
    if (!selectedPath.value)
      return
    saving.value = true
    try {
      await fetch(`/api/memory/${encodeURIComponent(selectedPath.value)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content.value }),
      })
      saved.value = true
      setTimeout(() => { saved.value = false }, 2000)
    }
    finally {
      saving.value = false
    }
  }

  onMounted(loadFiles)
  </script>

  <template>
    <div class="flex h-full min-h-0 border border-slate-200 dark:border-slate-700 rounded-lg overflow-hidden">
      <div class="w-64 flex-shrink-0 border-r border-slate-200 dark:border-slate-700 overflow-y-auto bg-slate-50 dark:bg-slate-800">
        <div class="px-3 py-2 text-xs font-semibold text-slate-500 uppercase tracking-wide border-b border-slate-200 dark:border-slate-700">
          Memory Files
        </div>
        <div v-if="files.length === 0" class="px-3 py-4 text-xs text-slate-400">
          No memory files found
        </div>
        <button
          v-for="f in files"
          :key="f.path"
          type="button"
          :class="[
            'w-full text-left px-3 py-1.5 text-xs truncate transition-colors',
            selectedPath === f.path
              ? 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300'
              : 'text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700',
          ]"
          @click="openFile(f.path)"
        >
          {{ f.name }}
        </button>
      </div>
      <div class="flex-1 flex flex-col min-w-0">
        <div v-if="!selectedPath" class="flex-1 flex items-center justify-center text-sm text-slate-400">
          Select a file to edit
        </div>
        <template v-else>
          <div class="flex items-center justify-between px-4 py-2 border-b border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900">
            <span class="text-xs font-mono text-slate-500 truncate">{{ selectedPath }}</span>
            <AppButton size="sm" :disabled="saving" @click="save">
              {{ saved ? 'Saved!' : saving ? 'Saving…' : 'Save' }}
            </AppButton>
          </div>
          <textarea
            v-model="content"
            class="flex-1 resize-none p-4 font-mono text-xs bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200 outline-none"
            spellcheck="false"
          />
        </template>
      </div>
    </div>
  </template>
  ```

- [ ] **4.4** Write server test for path validation `server/routes/memoryRoutes.test.ts`

  ```typescript
  import { describe, expect, it } from 'vitest'
  import express from 'express'
  import request from 'supertest'
  import { createMemoryRouter } from './memoryRoutes.js'

  function makeApp() {
    const app = express()
    app.use(express.json())
    app.use('/api', createMemoryRouter())
    return app
  }

  describe('memoryRoutes path validation', () => {
    it('rejects path traversal in GET', async () => {
      const app = makeApp()
      const res = await request(app).get('/api/memory/..%2F..%2Fetc%2Fpasswd')
      expect(res.status).toBe(400)
      expect(res.body.error).toMatch(/traversal/i)
    })

    it('rejects path traversal in PUT', async () => {
      const app = makeApp()
      const res = await request(app)
        .put('/api/memory/..%2F..%2Fetc%2Fpasswd')
        .send({ content: 'x' })
      expect(res.status).toBe(400)
    })
  })
  ```

- [ ] **4.5** Run

  ```bash
  pnpm test --run server/routes/memoryRoutes.test.ts
  pnpm typecheck
  ```

- [ ] **4.6** Commit

  ```
  feat(dx): add memory file browser with path-safe REST API (DX-6)
  ```

---

## Task 5: N-Gram Workflow Pattern Discovery (NE-3)

**Files:**
- `server/db/client.ts` — `workflow_patterns` table migration
- `server/analytics/ngrams.ts` — extraction logic
- `server/routes/agentRoutes.ts` — `GET /api/analytics/patterns` + trigger endpoint
- `server/index.ts` — run on startup

### Steps

- [ ] **5.1** Add `workflow_patterns` table to `server/db/client.ts`

  ```typescript
  db.exec(`
    CREATE TABLE IF NOT EXISTS workflow_patterns (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      tools TEXT NOT NULL UNIQUE,
      frequency INTEGER NOT NULL DEFAULT 1,
      last_seen_at TEXT NOT NULL
    )
  `)
  ```

- [ ] **5.2** Create `server/analytics/ngrams.ts`

  ```typescript
  import { readdir } from 'node:fs/promises'
  import { join } from 'node:path'
  import type { Database } from '../db/client.js'
  import { CLAUDE_PROJECTS_DIR } from '../paths.js'
  import { parseFullSession } from '../jsonlParser.js'

  const N = 3

  export function extractNgrams(toolSequence: string[]): Map<string, number> {
    const counts = new Map<string, number>()
    for (let i = 0; i <= toolSequence.length - N; i++) {
      const gram = toolSequence.slice(i, i + N).join(' → ')
      counts.set(gram, (counts.get(gram) ?? 0) + 1)
    }
    return counts
  }

  export async function discoverPatterns(db: Database): Promise<void> {
    const allCounts = new Map<string, number>()

    // Walk all project session files
    const projects = await readdir(CLAUDE_PROJECTS_DIR).catch(() => [])
    for (const project of projects) {
      const projectDir = join(CLAUDE_PROJECTS_DIR, project)
      const entries = await readdir(projectDir).catch(() => [])
      for (const entry of entries) {
        if (!entry.endsWith('.jsonl'))
          continue
        const sessionId = entry.replace('.jsonl', '')
        try {
          const messages = await parseFullSession(sessionId, false)
          const tools = messages.filter(m => m.role === 'tool_call').map(m => m.toolName ?? 'unknown')
          const grams = extractNgrams(tools)
          for (const [gram, count] of grams)
            allCounts.set(gram, (allCounts.get(gram) ?? 0) + count)
        }
        catch {
          // Skip unreadable sessions
        }
      }
    }

    // Store top 20 patterns
    const top20 = [...allCounts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 20)

    const upsert = db.prepare(`
      INSERT INTO workflow_patterns (tools, frequency, last_seen_at)
      VALUES (?, ?, ?)
      ON CONFLICT(tools) DO UPDATE SET
        frequency = excluded.frequency,
        last_seen_at = excluded.last_seen_at
    `)
    const now = new Date().toISOString()
    for (const [gram, freq] of top20)
      upsert.run(gram, freq, now)
  }
  ```

- [ ] **5.3** Add routes in `server/routes/agentRoutes.ts`

  ```typescript
  import { discoverPatterns } from '../analytics/ngrams.js'

  router.get('/analytics/patterns', (_req, res) => {
    const db = getDb()
    const rows = db.prepare(
      'SELECT tools, frequency, last_seen_at FROM workflow_patterns ORDER BY frequency DESC LIMIT 20',
    ).all() as Array<{ tools: string, frequency: number, last_seen_at: string }>
    res.json({ patterns: rows })
  })

  router.post('/analytics/patterns/refresh', async (_req, res) => {
    try {
      await discoverPatterns(getDb())
      res.json({ ok: true })
    }
    catch {
      res.status(500).json({ error: 'Pattern discovery failed' })
    }
  })
  ```

- [ ] **5.4** Trigger on startup in `server/index.ts` (fire-and-forget):

  ```typescript
  import { discoverPatterns } from './analytics/ngrams.js'

  // After DB is initialized:
  discoverPatterns(getDb()).catch(err => consola.warn('Pattern discovery error:', err))
  ```

- [ ] **5.5** Add "Suggested Workflow Patterns" panel to settings UI

  ```html
  <div class="mt-4 border-t pt-4">
    <h3 class="text-sm font-semibold mb-2">Workflow Patterns</h3>
    <p class="text-xs text-slate-400 mb-2">Top 3-tool sequences discovered across all sessions</p>
    <div v-if="patterns.length === 0" class="text-xs text-slate-400">No patterns discovered yet.</div>
    <ul class="space-y-1">
      <li
        v-for="p in patterns"
        :key="p.tools"
        class="text-xs font-mono bg-slate-100 dark:bg-slate-800 px-2 py-1 rounded flex justify-between"
      >
        <span>{{ p.tools }}</span>
        <span class="text-slate-400">×{{ p.frequency }}</span>
      </li>
    </ul>
    <button
      type="button"
      class="mt-2 text-xs px-2 py-1 rounded border border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-400"
      @click="refreshPatterns"
    >
      Refresh
    </button>
  </div>
  ```

  In script setup:

  ```typescript
  const patterns = ref<Array<{ tools: string, frequency: number }>>([])

  async function loadPatterns() {
    const res = await fetch('/api/analytics/patterns')
    if (res.ok) {
      const data = await res.json() as { patterns: typeof patterns.value }
      patterns.value = data.patterns
    }
  }

  async function refreshPatterns() {
    await fetch('/api/analytics/patterns/refresh', { method: 'POST' })
    await loadPatterns()
  }

  onMounted(loadPatterns)
  ```

- [ ] **5.6** Write Vitest test `server/analytics/ngrams.test.ts`

  ```typescript
  import { describe, expect, it } from 'vitest'
  import { extractNgrams } from './ngrams.js'

  describe('extractNgrams', () => {
    it('extracts trigrams from a sequence', () => {
      const seq = ['Read', 'Grep', 'Write', 'Bash']
      const counts = extractNgrams(seq)
      expect(counts.get('Read → Grep → Write')).toBe(1)
      expect(counts.get('Grep → Write → Bash')).toBe(1)
      expect(counts.size).toBe(2)
    })

    it('counts repeated trigrams', () => {
      const seq = ['Read', 'Write', 'Bash', 'Read', 'Write', 'Bash']
      const counts = extractNgrams(seq)
      expect(counts.get('Read → Write → Bash')).toBe(2)
    })

    it('returns empty map for short sequences', () => {
      expect(extractNgrams(['Read', 'Write']).size).toBe(0)
    })
  })
  ```

- [ ] **5.7** Run

  ```bash
  pnpm test --run server/analytics/ngrams.test.ts
  pnpm typecheck
  ```

- [ ] **5.8** Commit

  ```
  feat(analytics): add N-gram workflow pattern discovery with startup scan (NE-3)
  ```

---

## Task 6: Per-Agent Color/Emoji Identity (IP-1)

**Files:**
- `src/composables/useAgentIdentity.ts` — new composable
- `src/components/AgentRow.vue` — add color dot + emoji
- `src/components/AgentCard.vue` — add color dot + emoji
- `src/components/AgentModal.vue` — add identity in header

### Steps

- [ ] **6.1** Create `src/composables/useAgentIdentity.ts`

  ```typescript
  import { ref } from 'vue'

  interface AgentIdentity {
    color: string
    emoji: string
  }

  const STORAGE_KEY = 'agent-identities'

  const COLORS = ['#3b82f6', '#8b5cf6', '#10b981', '#f59e0b', '#ef4444', '#06b6d4', '#f97316', '#84cc16']
  const EMOJIS = ['🤖', '🦾', '🧠', '⚡', '🔬', '🛠️', '🎯', '🚀', '🦊', '🐙']

  function load(): Record<string, AgentIdentity> {
    try {
      return JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
    }
    catch {
      return {}
    }
  }

  function persist(store: Record<string, AgentIdentity>): void {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(store))
  }

  function deterministicIndex(str: string, len: number): number {
    let hash = 0
    for (const ch of str)
      hash = ((hash << 5) - hash + ch.charCodeAt(0)) | 0
    return Math.abs(hash) % len
  }

  const identities = ref<Record<string, AgentIdentity>>(load())

  export function useAgentIdentity() {
    function getIdentity(projectPath: string): AgentIdentity {
      if (!identities.value[projectPath]) {
        identities.value[projectPath] = {
          color: COLORS[deterministicIndex(projectPath, COLORS.length)],
          emoji: EMOJIS[deterministicIndex(projectPath + '!', EMOJIS.length)],
        }
        persist(identities.value)
      }
      return identities.value[projectPath]
    }

    function setIdentity(projectPath: string, identity: Partial<AgentIdentity>): void {
      identities.value[projectPath] = { ...getIdentity(projectPath), ...identity }
      persist(identities.value)
    }

    return { getIdentity, setIdentity }
  }
  ```

- [ ] **6.2** Add color dot + emoji to `src/components/AgentRow.vue`

  In `<script setup>`:

  ```typescript
  import { useAgentIdentity } from '../composables/useAgentIdentity'
  const { getIdentity } = useAgentIdentity()
  ```

  In template, prepend to the project name cell:

  ```html
  <span class="mr-1 text-sm">{{ getIdentity(agent.projectPath).emoji }}</span>
  <span
    :style="{ backgroundColor: getIdentity(agent.projectPath).color }"
    class="inline-block w-2 h-2 rounded-full mr-1 flex-shrink-0"
  />
  ```

- [ ] **6.3** Add to `src/components/AgentCard.vue` — in the card header alongside the project name:

  ```html
  <span class="mr-1">{{ getIdentity(agent.projectPath).emoji }}</span>
  ```

- [ ] **6.4** Add to `src/components/AgentModal.vue` — in the header span beside `agent.projectName`:

  ```html
  <span class="mr-1">{{ getIdentity(agent.projectPath).emoji }}</span>
  ```

  Import in both components:

  ```typescript
  import { useAgentIdentity } from '../composables/useAgentIdentity'
  const { getIdentity } = useAgentIdentity()
  ```

- [ ] **6.5** Write composable test `src/composables/useAgentIdentity.test.ts`

  ```typescript
  import { describe, expect, it, beforeEach } from 'vitest'

  // localStorage stub
  const store: Record<string, string> = {}
  global.localStorage = {
    getItem: (k: string) => store[k] ?? null,
    setItem: (k: string, v: string) => { store[k] = v },
    removeItem: (k: string) => { delete store[k] },
    clear: () => { Object.keys(store).forEach(k => delete store[k]) },
    length: 0,
    key: () => null,
  }

  describe('useAgentIdentity', () => {
    beforeEach(() => global.localStorage.clear())

    it('assigns deterministic color and emoji', async () => {
      const { useAgentIdentity } = await import('./useAgentIdentity')
      const { getIdentity } = useAgentIdentity()
      const id1 = getIdentity('/projects/foo')
      const id2 = getIdentity('/projects/foo')
      expect(id1.color).toBe(id2.color)
      expect(id1.emoji).toBe(id2.emoji)
    })

    it('assigns string-type color and emoji', async () => {
      const { useAgentIdentity } = await import('./useAgentIdentity')
      const { getIdentity } = useAgentIdentity()
      const a = getIdentity('/projects/alpha')
      expect(typeof a.color).toBe('string')
      expect(typeof a.emoji).toBe('string')
    })
  })
  ```

- [ ] **6.6** Run

  ```bash
  pnpm test --run src/composables/useAgentIdentity.test.ts
  pnpm typecheck
  ```

- [ ] **6.7** Commit

  ```
  feat(ux): add per-agent color/emoji identity via localStorage (IP-1)
  ```

---

## Task 7: Python Statusline CLI (IP-2)

**Files:**
- `scripts/statusline.py` — standalone Python 3 script

### Steps

- [ ] **7.1** Create `scripts/statusline.py`

  ```python
  #!/usr/bin/env python3
  """
  Agent Dashboard statusline — prints a one-line summary of running agents.

  Usage:
      python3 scripts/statusline.py
      python3 scripts/statusline.py --format json

  Environment:
      DASHBOARD_API_URL   Base URL of the dashboard (default: http://127.0.0.1:13120)
      DASHBOARD_API_TOKEN Bearer token if auth is enabled (optional)

  Shell integration (zsh) — add to ~/.zshrc:
      _agent_status() {
          local out
          out=$(python3 /path/to/agent-dashboard/scripts/statusline.py 2>/dev/null)
          [[ -n "$out" ]] && echo " [$out]"
      }
      PROMPT='%n@%m %~$(_agent_status) %# '

  Shell integration (bash) — add to ~/.bashrc:
      _agent_status() {
          python3 /path/to/agent-dashboard/scripts/statusline.py 2>/dev/null
      }
      PROMPT_COMMAND='export PS1="\\u@\\h \\w [$(_agent_status)] \\$ "'
  """

  import json
  import os
  import sys
  import urllib.request
  import urllib.error
  from typing import Any

  BASE_URL = os.environ.get("DASHBOARD_API_URL", "http://127.0.0.1:13120")
  TOKEN = os.environ.get("DASHBOARD_API_TOKEN", "")


  def fetch_agents() -> list[dict[str, Any]]:
      url = f"{BASE_URL}/api/agents"
      req = urllib.request.Request(url)
      if TOKEN:
          req.add_header("Authorization", f"Bearer {TOKEN}")
      try:
          with urllib.request.urlopen(req, timeout=2) as resp:
              return json.loads(resp.read().decode())
      except (urllib.error.URLError, json.JSONDecodeError):
          return []


  def summarize(agents: list[dict[str, Any]]) -> dict[str, Any]:
      active = sum(1 for a in agents if a.get("status") == "active")
      cost_per_hour = sum(
          a.get("costEstimate", 0) for a in agents if a.get("status") == "active"
      )
      total_tokens = sum(
          sum(a.get("tokenUsage", {}).values()) for a in agents
      )
      return {
          "active": active,
          "total": len(agents),
          "cost_per_hour": cost_per_hour,
          "total_tokens": total_tokens,
      }


  def format_statusline(s: dict[str, Any]) -> str:
      tok_k = s["total_tokens"] / 1000
      cost = s["cost_per_hour"]
      return f"⚡ {s['active']} active | ${cost:.2f}/h | {tok_k:.0f}K tok"


  def main() -> None:
      use_json = (
          "--format" in sys.argv
          and sys.argv.index("--format") + 1 < len(sys.argv)
          and sys.argv[sys.argv.index("--format") + 1] == "json"
      )
      agents = fetch_agents()
      summary = summarize(agents)
      if use_json:
          print(json.dumps(summary))
      else:
          print(format_statusline(summary))


  if __name__ == "__main__":
      main()
  ```

- [ ] **7.2** Make executable

  ```bash
  chmod +x scripts/statusline.py
  ```

- [ ] **7.3** Smoke test (requires running dashboard):

  ```bash
  python3 scripts/statusline.py
  # Expected output: ⚡ 2 active | $0.14/h | 32K tok

  python3 scripts/statusline.py --format json
  # Expected output: {"active": 2, "total": 3, "cost_per_hour": 0.14, "total_tokens": 32000}
  ```

  When the dashboard is not running, the script exits silently with no output (stderr suppressed by `2>/dev/null` in the shell integration).

- [ ] **7.4** Commit

  ```
  feat(cli): add Python statusline script for shell PS1 integration (IP-2)
  ```

---

## Task 8: Claude Pro/Max Quota Tracking (CI-8)

**Files:**
- `server/routes/agentRoutes.ts` — `GET /api/quota` endpoint
- `src/App.vue` — quota progress bar in header

### Steps

- [ ] **8.1** Add `GET /api/quota` to `server/routes/agentRoutes.ts`

  ```typescript
  import { homedir } from 'node:os'

  router.get('/quota', async (_req, res) => {
    const usageDir = join(homedir(), '.claude', 'usage-data')
    try {
      const files = await readdir(usageDir)
      // Find the most recent JSON usage file
      const jsonFiles = files.filter(f => f.endsWith('.json')).sort().reverse()
      if (jsonFiles.length === 0) {
        res.json({ limit: null })
        return
      }
      const raw = await readFile(join(usageDir, jsonFiles[0]), 'utf8')
      const data = JSON.parse(raw) as {
        periodStart?: string
        periodEnd?: string
        tokensUsed?: number
        limit?: number | null
      }
      res.json({
        periodStart: data.periodStart ?? null,
        periodEnd: data.periodEnd ?? null,
        tokensUsed: data.tokensUsed ?? 0,
        limit: data.limit ?? null,
      })
    }
    catch {
      // usage-data absent or unreadable — return graceful null
      res.json({ limit: null })
    }
  })
  ```

  Add the missing imports at the top of `agentRoutes.ts` if not present:

  ```typescript
  import { readdir, readFile } from 'node:fs/promises'
  import { join } from 'node:path'
  import { homedir } from 'node:os'
  ```

- [ ] **8.2** Add quota progress bar to `src/App.vue`

  In `<script setup>`:

  ```typescript
  interface QuotaInfo {
    periodStart: string | null
    periodEnd: string | null
    tokensUsed: number
    limit: number | null
  }

  const quota = ref<QuotaInfo | null>(null)

  async function fetchQuota() {
    const res = await fetch('/api/quota')
    if (res.ok)
      quota.value = await res.json() as QuotaInfo
  }

  onMounted(fetchQuota)
  ```

  In the header template, add after the existing stats:

  ```html
  <div v-if="quota && quota.limit" class="flex items-center gap-1.5" :title="`${quota.tokensUsed.toLocaleString()} / ${quota.limit.toLocaleString()} tokens`">
    <span class="text-[10px] text-slate-400">Quota</span>
    <div class="w-20 h-1.5 bg-slate-200 dark:bg-slate-700 rounded-full overflow-hidden">
      <div
        class="h-full rounded-full transition-all"
        :class="{
          'bg-red-500': quota.tokensUsed / quota.limit > 0.9,
          'bg-yellow-500': quota.tokensUsed / quota.limit > 0.7 && quota.tokensUsed / quota.limit <= 0.9,
          'bg-green-500': quota.tokensUsed / quota.limit <= 0.7,
        }"
        :style="{ width: `${Math.min(100, Math.round(quota.tokensUsed / quota.limit * 100))}%` }"
      />
    </div>
    <span class="text-[10px] text-slate-400">{{ Math.round(quota.tokensUsed / quota.limit * 100) }}%</span>
  </div>
  ```

- [ ] **8.3** Run

  ```bash
  pnpm typecheck
  ```

- [ ] **8.4** Commit

  ```
  feat(monitoring): add Claude quota tracking with header progress bar (CI-8)
  ```

---

## Task 9: PWA Support (IP-3)

**Files:**
- `vite.config.ts` — add VitePWA plugin
- `public/icon-192.png`, `public/icon-512.png` — app icons

### Steps

- [ ] **9.1** Install vite-plugin-pwa

  ```bash
  pnpm add -D vite-plugin-pwa
  ```

  Expected output: `vite-plugin-pwa` added to `devDependencies` in `package.json`.

- [ ] **9.2** Read `vite.config.ts` first, then add the VitePWA plugin

  Add import at the top of `vite.config.ts`:

  ```typescript
  import { VitePWA } from 'vite-plugin-pwa'
  ```

  Add to the `plugins` array (alongside the existing Vue and other plugins):

  ```typescript
  VitePWA({
    registerType: 'autoUpdate',
    includeAssets: ['favicon.ico', 'apple-touch-icon.png', 'icon-192.png', 'icon-512.png'],
    manifest: {
      name: 'Agent Dashboard',
      short_name: 'Agents',
      description: 'Real-time monitoring dashboard for Claude Code agents',
      theme_color: '#1e293b',
      background_color: '#0f172a',
      display: 'standalone',
      start_url: '/',
      icons: [
        {
          src: '/icon-192.png',
          sizes: '192x192',
          type: 'image/png',
        },
        {
          src: '/icon-512.png',
          sizes: '512x512',
          type: 'image/png',
        },
        {
          src: '/icon-512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'any maskable',
        },
      ],
    },
    workbox: {
      // Cache static assets only — never cache API responses
      globPatterns: ['**/*.{js,css,html,ico,woff2}'],
      navigateFallback: null,
      runtimeCaching: [],
    },
  }),
  ```

- [ ] **9.3** Add icon placeholders to `public/`

  Place 192×192 and 512×512 PNG icon files at `public/icon-192.png` and `public/icon-512.png`. These can be generated from the project's existing favicon using ImageMagick (`convert favicon.ico -resize 192x192 public/icon-192.png`) or any image editor. The PWA will install without them but the homescreen icon will fall back to a generic browser icon.

- [ ] **9.4** Build and verify PWA artifacts

  ```bash
  pnpm build
  ls dist/manifest.webmanifest dist/sw.js
  ```

  Expected: both files present in `dist/`.

- [ ] **9.5** Run typecheck

  ```bash
  pnpm typecheck
  ```

- [ ] **9.6** Commit

  ```
  feat(pwa): add PWA support with vite-plugin-pwa, service worker, and app manifest (IP-3)
  ```
