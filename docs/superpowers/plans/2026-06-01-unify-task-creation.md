# Unify Task Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the pipeline view's two creation buttons (`+ New Task`, `+ Backlog`) with one `+ New Task` button that opens a single-screen create form offering **Create** (lands in backlog) and **Create & Refine** (lands in backlog, then opens the refinement chat).

**Architecture:** `BacklogForm.vue` becomes one screen: a project dropdown (auto-fills cwd via `suggestFolders`) plus the existing task fields, exposing two emits — `created` and `createdAndRefine` — off one identical `createTask()` call. `App.vue` decides the post-create action (select vs open chat). The two-step `ProjectStep`→`DetailsStep` wizard is removed for this flow (`DetailsStep.vue` deleted; `ProjectStep.vue` kept for `SpawnDialog`). `RefinementChat.vue`'s now-dead standalone-create path is removed so it always opens on an existing task.

**Tech Stack:** Vue 3 (`<script setup>` SFCs), TypeScript, Vitest + @vue/test-utils, pnpm.

**Spec:** `docs/superpowers/specs/2026-06-01-unify-task-creation-design.md`

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `src/components/BacklogForm.vue` | Rewrite | Single-screen create form; project dropdown + task fields; emits `created` / `createdAndRefine`. |
| `src/components/BacklogForm.test.ts` | Rewrite | Cover single-screen behavior + both emits. |
| `src/components/backlog/DetailsStep.vue` | Delete | Fields absorbed into BacklogForm. |
| `src/components/backlog/ProjectStep.vue` | Keep untouched | Still imported by `SpawnDialog.vue`. |
| `src/App.vue` | Modify | Remove `+ Backlog` button; repoint `openNewTask`; wire `createdAndRefine`; rename modal heading. |
| `src/components/RefinementChat.vue` | Modify | Remove null-task standalone-create branch + empty-state project picker. Always opened with a task. |
| `src/components/__tests__/RefinementChat.createflow.test.ts` | Delete | Covers removed standalone-create path. |
| `src/components/__tests__/RefinementChat.cwd.test.ts` | Delete | Covers removed empty-state cwd picker. |

**Verification commands** (used throughout):
- Single test file: `pnpm test -- src/components/BacklogForm.test.ts`
- Full unit run: `pnpm test`
- Types: `pnpm typecheck`

---

## Task 1: Rewrite BacklogForm as a single-screen form

**Files:**
- Modify: `src/components/BacklogForm.vue` (full rewrite)
- Test: `src/components/BacklogForm.test.ts` (full rewrite)

- [ ] **Step 1: Replace the test file with single-screen specs**

Replace the entire contents of `src/components/BacklogForm.test.ts` with:

```ts
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

vi.mock('../composables/useProjects', () => ({
  useProjects: () => ({
    projects: ref([
      { id: 'p1', slug: 'web', name: 'Web', folders: [{ id: 'f1', projectId: 'p1', path: '/repos/web', isDefault: true, createdAt: '' }], createdAt: '', updatedAt: '' },
      { id: 'p2', slug: 'api', name: 'API', folders: [], createdAt: '', updatedAt: '' },
    ]),
    refetch: vi.fn(),
  }),
  createProject: vi.fn(),
  deleteProject: vi.fn(),
}))

vi.mock('../composables/useSpawners', () => ({
  useSpawners: () => ({ spawners: ref([{ id: 's1', name: 'Claude default', slug: 'claude-default', command: 'claude', args: [], env: {}, adapterType: 'claude', adapterConfig: {}, builtIn: true, createdAt: '', updatedAt: '' }]) }),
}))

const createTaskMock = vi.fn().mockResolvedValue({ id: 't1', slug: 'demo', title: 'Demo' } as unknown)
vi.mock('../composables/useTasks', () => ({
  createTask: (input: unknown) => createTaskMock(input),
}))

vi.mock('../composables/useProjectFolders', () => ({
  suggestFolders: vi.fn().mockResolvedValue([
    { id: 'f1', projectId: 'p1', path: '/repos/web', isDefault: true, createdAt: '' },
  ]),
  createFolder: vi.fn(),
}))

import BacklogForm from './BacklogForm.vue'

describe('BacklogForm single-screen', () => {
  it('renders the form fields and a project dropdown on one screen', () => {
    const wrapper = mount(BacklogForm)
    expect(wrapper.find('[data-testid="backlog-project-select"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="details-title"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="details-cwd"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="details-submit"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="details-submit-refine"]').exists()).toBe(true)
  })

  it('auto-derives slug from the title', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('My New Task')
    const slug = wrapper.get('[data-testid="details-slug"]').element as HTMLInputElement
    expect(slug.value).toBe('my-new-task')
  })

  it('auto-fills cwd from the default folder when a project is selected', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('p1')
    await flushPromises()
    const cwd = wrapper.get('[data-testid="details-cwd"]').element as HTMLInputElement
    expect(cwd.value).toBe('/repos/web')
  })

  it('disables both submit buttons until title, slug, and cwd are filled', async () => {
    const wrapper = mount(BacklogForm)
    expect(wrapper.get('[data-testid="details-submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/x')
    expect(wrapper.get('[data-testid="details-submit"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeUndefined()
  })

  it('expands QuickCreateProjectPanel when "+ Create new project" is selected', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('__create__')
    expect(wrapper.findComponent({ name: 'QuickCreateProjectPanel' }).exists()).toBe(true)
  })

  it('emits created with the new backlog task via Create', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('p1')
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await wrapper.get('[data-testid="details-submit"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({
      projectId: 'p1',
      title: 'Demo task',
      slug: 'demo-task',
      cwd: '/repos/web',
    }))
    expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ stage: expect.anything() }))
    expect(wrapper.emitted('created')).toBeTruthy()
    expect(wrapper.emitted('createdAndRefine')).toBeFalsy()
  })

  it('emits createdAndRefine with the new task via Create & Refine', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('Refine me')
    await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/y')
    await wrapper.get('[data-testid="details-submit-refine"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('createdAndRefine')).toBeTruthy()
    expect(wrapper.emitted('created')).toBeFalsy()
  })

  it('omits projectId when no project is selected', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('No project')
    await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/x')
    await wrapper.get('[data-testid="details-submit"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ projectId: expect.anything() }))
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test -- src/components/BacklogForm.test.ts`
Expected: FAIL — the current wizard markup has no `backlog-project-select` / `details-submit-refine`, and there is no `createdAndRefine` emit.

- [ ] **Step 3: Rewrite BacklogForm.vue as a single screen**

Replace the entire contents of `src/components/BacklogForm.vue` with:

```vue
<script setup lang="ts">
import type { PipelineTask, Project, ProjectFolder } from '../types'
import { computed, ref, watch } from 'vue'
import { suggestFolders } from '../composables/useProjectFolders'
import { useProjects } from '../composables/useProjects'
import { useSpawners } from '../composables/useSpawners'
import { createTask } from '../composables/useTasks'
import { slugify } from '../utils/validation'
import PermissionTemplatePicker from './PermissionTemplatePicker.vue'
import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'

const emit = defineEmits<{
  created: [task: PipelineTask]
  createdAndRefine: [task: PipelineTask]
}>()

type PermissionTemplateId = 'research_only' | 'test_only' | 'review_only' | 'feature_implementation'

const { projects, refetch } = useProjects()
const { spawners } = useSpawners()

const projectChoice = ref<string>('')
const showCreate = ref(false)
const title = ref('')
const slug = ref('')
const description = ref('')
const cwd = ref('')
const priority = ref<'high' | 'medium' | 'low'>('medium')
const selectedTemplate = ref<PermissionTemplateId | null>('feature_implementation')
const selectedSpawnerId = ref<string>('')
const folderSuggestions = ref<ProjectFolder[]>([])
const isSubmitting = ref(false)
const errorMsg = ref('')

const fieldClass = 'w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-blue-500'

const sortedProjects = computed(() =>
  projects.value.slice().sort((a, b) => a.name.localeCompare(b.name)),
)

const canSubmit = computed(() =>
  !!title.value.trim() && !!slug.value.trim() && !!cwd.value.trim() && !isSubmitting.value,
)

watch(projectChoice, async (v) => {
  if (v === '__create__') {
    showCreate.value = true
    return
  }
  showCreate.value = false
  if (!v) {
    folderSuggestions.value = []
    return
  }
  try {
    const suggestions = await suggestFolders(v)
    folderSuggestions.value = suggestions
    const def = suggestions.find(f => f.isDefault) ?? suggestions[0]
    if (def)
      cwd.value = def.path
  }
  catch {
    folderSuggestions.value = []
  }
})

function onTitleInput(e: Event): void {
  const value = (e.target as HTMLInputElement).value
  const previousSlug = slugify(title.value)
  title.value = value
  if (!slug.value || slug.value === previousSlug)
    slug.value = slugify(value)
}

function onSlugInput(e: Event): void {
  slug.value = (e.target as HTMLInputElement).value
}

function onCwdInput(e: Event): void {
  cwd.value = (e.target as HTMLInputElement).value
}

function onProjectCreated(p: Project): void {
  showCreate.value = false
  void refetch?.()
  projectChoice.value = p.id
}

function onQuickCreateCancel(): void {
  showCreate.value = false
  projectChoice.value = ''
}

async function buildTask(): Promise<PipelineTask | null> {
  if (isSubmitting.value)
    return null
  const t = title.value.trim()
  const s = slug.value.trim()
  const c = cwd.value.trim()
  if (!t || !s || !c) {
    errorMsg.value = 'Title, slug, and working directory are required.'
    return null
  }
  isSubmitting.value = true
  errorMsg.value = ''
  try {
    const projectId = projectChoice.value && projectChoice.value !== '__create__' ? projectChoice.value : ''
    return await createTask({
      slug: s,
      title: t,
      description: description.value.trim() || undefined,
      cwd: c,
      priority: priority.value,
      template: selectedTemplate.value ?? undefined,
      ...(projectId ? { projectId } : {}),
      ...(selectedSpawnerId.value ? { spawnerId: selectedSpawnerId.value } : {}),
    })
  }
  catch (err: unknown) {
    errorMsg.value = err instanceof Error ? err.message : 'Failed to create task'
    return null
  }
  finally {
    isSubmitting.value = false
  }
}

async function onCreate(): Promise<void> {
  const task = await buildTask()
  if (task)
    emit('created', task)
}

async function onCreateAndRefine(): Promise<void> {
  const task = await buildTask()
  if (task)
    emit('createdAndRefine', task)
}
</script>

<template>
  <form data-testid="backlog-form" class="space-y-4" @submit.prevent="onCreate">
    <div class="flex flex-col gap-1">
      <label for="backlog-project" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Project</label>
      <select
        id="backlog-project"
        v-model="projectChoice"
        data-testid="backlog-project-select"
        :class="fieldClass"
      >
        <option value="">
          No project
        </option>
        <option
          v-for="p in sortedProjects"
          :key="p.id"
          :value="p.id"
        >
          {{ p.name }}
        </option>
        <option value="__create__">
          + Create new project…
        </option>
      </select>
    </div>

    <div v-if="showCreate" data-testid="quick-create-panel">
      <QuickCreateProjectPanel
        :spawners="spawners"
        @created="onProjectCreated"
        @cancel="onQuickCreateCancel"
      />
    </div>

    <div class="flex flex-col gap-1">
      <label for="details-title" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Title</label>
      <input
        id="details-title"
        data-testid="details-title"
        :value="title"
        placeholder="What should the agent do?"
        :class="fieldClass"
        @input="onTitleInput"
      >
    </div>

    <div class="flex flex-col gap-1">
      <label for="details-slug" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Slug</label>
      <input
        id="details-slug"
        data-testid="details-slug"
        :value="slug"
        placeholder="task-slug"
        :class="fieldClass"
        @input="onSlugInput"
      >
    </div>

    <div class="flex flex-col gap-1">
      <label for="details-cwd" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Working Directory</label>
      <input
        id="details-cwd"
        data-testid="details-cwd"
        :value="cwd"
        list="details-cwd-list"
        placeholder="/path/to/project"
        :class="fieldClass"
        @input="onCwdInput"
      >
      <datalist id="details-cwd-list">
        <option v-for="folder in folderSuggestions" :key="folder.id" :value="folder.path">
          {{ folder.label || folder.path }}
        </option>
      </datalist>
    </div>

    <AppInput v-model="description" type="textarea" :rows="3" label="Description" placeholder="Additional context (optional)" />

    <div class="flex flex-col gap-1">
      <label for="details-priority" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Priority</label>
      <select id="details-priority" v-model="priority" :class="fieldClass">
        <option value="high">
          High
        </option>
        <option value="medium">
          Medium
        </option>
        <option value="low">
          Low
        </option>
      </select>
    </div>

    <div class="flex flex-col gap-1">
      <label for="details-spawner" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Spawner</label>
      <select id="details-spawner" v-model="selectedSpawnerId" :class="fieldClass">
        <option value="">
          {{ projectChoice && projectChoice !== '__create__' ? 'Project default' : 'Claude default' }}
        </option>
        <option v-for="s in spawners" :key="s.id" :value="s.id">
          {{ s.name }}{{ s.builtIn ? ' (built-in)' : '' }}
        </option>
      </select>
    </div>

    <PermissionTemplatePicker v-model="selectedTemplate" />

    <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400">
      {{ errorMsg }}
    </p>

    <div class="flex justify-end gap-2">
      <AppButton
        variant="secondary"
        :disabled="!canSubmit"
        data-testid="details-submit-refine"
        @click="onCreateAndRefine"
      >
        {{ isSubmitting ? 'Creating…' : 'Create & Refine' }}
      </AppButton>
      <AppButton
        variant="primary"
        :disabled="!canSubmit"
        data-testid="details-submit"
        @click="onCreate"
      >
        {{ isSubmitting ? 'Creating…' : 'Create' }}
      </AppButton>
    </div>
  </form>
</template>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test -- src/components/BacklogForm.test.ts`
Expected: PASS (all 8 specs).

- [ ] **Step 5: Typecheck**

Run: `pnpm typecheck`
Expected: no errors. (If `useProjects` does not expose `refetch`, drop the `void refetch?.()` line — `refetch` is optional-chained so this only matters for the type. The current `BacklogForm.vue` already calls `refetch?.()`, so it is safe.)

- [ ] **Step 6: Commit**

```bash
git add src/components/BacklogForm.vue src/components/BacklogForm.test.ts
git commit -m "feat(pipeline): single-screen task create form with Create & Refine"
```

---

## Task 2: Delete the obsolete DetailsStep wizard step

**Files:**
- Delete: `src/components/backlog/DetailsStep.vue`

`ProjectStep.vue` is intentionally NOT deleted — `SpawnDialog.vue` still imports it.

- [ ] **Step 1: Confirm DetailsStep is no longer referenced**

Run: `grep -rn "DetailsStep" src`
Expected: no matches (Task 1 removed the BacklogForm import).

- [ ] **Step 2: Delete the file**

```bash
git rm src/components/backlog/DetailsStep.vue
```

- [ ] **Step 3: Confirm ProjectStep is still used (must NOT be deleted)**

Run: `grep -rn "ProjectStep" src`
Expected: at least one match in `src/components/SpawnDialog.vue`.

- [ ] **Step 4: Typecheck**

Run: `pnpm typecheck`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(pipeline): remove unused DetailsStep wizard step"
```

---

## Task 3: Wire App.vue to the unified entry point

**Files:**
- Modify: `src/App.vue` (button block ~238-246, `openNewTask` ~120-123, modal block ~358-375)

- [ ] **Step 1: Remove the `+ Backlog` button**

Delete this block (currently at `src/App.vue:238-246`):

```vue
            <button
              v-if="activeView === 'pipeline'"
              type="button"
              class="bg-raised text-fg border border-line rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
              data-testid="open-backlog-form"
              @click="openBacklogForm"
            >
              + Backlog
            </button>
```

The `+ New Task` button immediately above it (`@click="openNewTask"`) stays.

- [ ] **Step 2: Repoint `openNewTask` to open the create modal**

Replace this block (currently at `src/App.vue:120-127`):

```ts
function openNewTask() {
  activeConceptTask.value = null
  showRefinementChat.value = true
}

function openBacklogForm() {
  showBacklogForm.value = true
}
```

with:

```ts
function openNewTask() {
  showBacklogForm.value = true
}
```

(`openBacklogForm` is removed — its only caller was the deleted button.)

- [ ] **Step 3: Add the Create & Refine handler**

`onBacklogTaskCreated` already exists at `src/App.vue:129-132`:

```ts
function onBacklogTaskCreated(task: PipelineTask) {
  showBacklogForm.value = false
  selectTask(task)
}
```

Immediately after it, add:

```ts
function onCreateTaskAndRefine(task: PipelineTask) {
  showBacklogForm.value = false
  activeConceptTask.value = task
  showRefinementChat.value = true
}
```

- [ ] **Step 4: Wire the new emit + rename the modal heading**

Replace the modal contents (currently at `src/App.vue:358-375`):

```vue
    <AppModal :open="showBacklogForm" @close="showBacklogForm = false">
      <div class="bg-app border border-line rounded-lg p-5 w-full max-w-xl">
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-base font-semibold text-fg">
            New Backlog Task
          </h2>
          <button
            type="button"
            class="bg-transparent border-none text-fg-mute text-base cursor-pointer px-2 py-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-fg"
            data-testid="close-backlog-form"
            @click="showBacklogForm = false"
          >
            ✕
          </button>
        </div>
        <BacklogForm @created="onBacklogTaskCreated" />
      </div>
    </AppModal>
```

with:

```vue
    <AppModal :open="showBacklogForm" @close="showBacklogForm = false">
      <div class="bg-app border border-line rounded-lg p-5 w-full max-w-xl">
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-base font-semibold text-fg">
            New Task
          </h2>
          <button
            type="button"
            class="bg-transparent border-none text-fg-mute text-base cursor-pointer px-2 py-1 rounded hover:bg-slate-200 dark:hover:bg-slate-700 hover:text-fg"
            data-testid="close-backlog-form"
            @click="showBacklogForm = false"
          >
            ✕
          </button>
        </div>
        <BacklogForm @created="onBacklogTaskCreated" @created-and-refine="onCreateTaskAndRefine" />
      </div>
    </AppModal>
```

- [ ] **Step 5: Typecheck**

Run: `pnpm typecheck`
Expected: no errors. If TS reports `openBacklogForm` is still referenced, search `grep -rn "openBacklogForm" src` and remove the stray reference.

- [ ] **Step 6: Full unit run**

Run: `pnpm test`
Expected: PASS except the two RefinementChat tests removed in Task 4 (run Task 4 next). If green already, even better.

- [ ] **Step 7: Commit**

```bash
git add src/App.vue
git commit -m "feat(pipeline): single + New Task entry replaces dual create buttons"
```

---

## Task 4: Remove RefinementChat's standalone-create path

**Files:**
- Modify: `src/components/RefinementChat.vue`
- Delete: `src/components/__tests__/RefinementChat.createflow.test.ts`
- Delete: `src/components/__tests__/RefinementChat.cwd.test.ts`

RefinementChat is now always opened with a non-null `task` (from kanban `@open-chat` or the new Create & Refine handler). The null-task create branch and the empty-state project picker are dead.

- [ ] **Step 1: Delete the two tests that cover the removed path**

```bash
git rm src/components/__tests__/RefinementChat.createflow.test.ts src/components/__tests__/RefinementChat.cwd.test.ts
```

- [ ] **Step 2: Simplify `handleSend` (remove both null-task branches)**

Replace `handleSend` (currently at `src/components/RefinementChat.vue:246-275`):

```ts
async function handleSend() {
  const msg = inputText.value.trim()
  if (!msg || isStreaming.value)
    return
  if (currentTask.value === null) {
    if (!dlg.cwd.value.trim()) {
      cwdError.value = 'Please choose a working directory first'
      return
    }
    cwdError.value = null
  }
  const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined
  inputText.value = ''
  pendingImages.value = []
  await nextTick(autoResize)
  if (currentTask.value === null) {
    const newTask = await createTask({
      slug: `concept-${Date.now()}`,
      title: 'New Task',
      cwd: dlg.cwd.value.trim(),
      stage: 'concept',
    })
    // Mark the id BEFORE the reactive switch so the open/id watcher skips its
    // loadHistory for this task — sendMessage below is the source of truth.
    justCreatedId.value = newTask.id
    currentTask.value = newTask
    emit('taskCreated', newTask)
  }
  await sendMessage(msg, images)
}
```

with:

```ts
async function handleSend() {
  const msg = inputText.value.trim()
  if (!msg || isStreaming.value || !currentTask.value)
    return
  const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined
  inputText.value = ''
  pendingImages.value = []
  await nextTick(autoResize)
  await sendMessage(msg, images)
}
```

- [ ] **Step 3: Remove the empty-state project picker from the template**

Delete this block (currently at `src/components/RefinementChat.vue:331-403`) — the entire `<!-- Project picker → derives working directory -->` container, including the project `<select>`, the inline `QuickCreateProjectPanel`, the folder `<select>`, the derived-cwd `<p>`, and the `cwdError` `<p>`:

```vue
          <!-- Project picker → derives working directory -->
          <div class="flex flex-col gap-2 w-full max-w-[480px]">
            <div class="flex flex-col gap-1">
              <label
                for="refine-project-select"
                class="text-xs font-medium text-fg-mute text-left"
              >
                Project
              </label>
              <select
                id="refine-project-select"
                v-model="projectChoice"
                data-testid="cwd-project-select"
                class="w-full px-3 py-2 rounded-xl border border-line bg-raised text-fg text-[13px] focus:outline-none focus:border-blue-400 dark:focus:border-blue-500 transition-colors"
                :class="{ 'border-red-400 dark:border-red-500': cwdError }"
              >
                <option value="" disabled>
                  Choose a project…
                </option>
                <option
                  v-for="p in sortedProjects"
                  :key="p.id"
                  :value="p.id"
                  :disabled="p.folderCount === 0"
                >
                  {{ p.name }}{{ p.folderCount === 0 ? ' — no folder, add one in /settings/projects' : '' }}
                </option>
                <option value="__create__">
                  + Create new project…
                </option>
              </select>
            </div>

            <QuickCreateProjectPanel
              v-if="showQuickCreate"
              :spawners="spawners"
              @created="onProjectCreated"
              @cancel="onQuickCreateCancel"
            />

            <!-- Folder picker — only when the project has more than one -->
            <div v-if="folderPickerVisible" class="flex flex-col gap-1">
              <label
                for="refine-folder-select"
                class="text-xs font-medium text-fg-mute text-left"
              >
                Folder
              </label>
              <select
                id="refine-folder-select"
                :value="dlg.selectedFolderId.value ?? ''"
                data-testid="cwd-folder-select"
                class="w-full px-3 py-2 rounded-xl border border-line bg-raised text-fg text-[13px] focus:outline-none focus:border-blue-400 dark:focus:border-blue-500 transition-colors"
                @change="dlg.selectFolder(($event.target as HTMLSelectElement).value)"
              >
                <option v-for="f in dlg.folders.value" :key="f.id" :value="f.id">
                  {{ f.label || f.path }}{{ f.isDefault ? ' (default)' : '' }}
                </option>
              </select>
            </div>

            <!-- Derived working directory (read-only) -->
            <p
              v-if="dlg.cwd.value"
              data-testid="cwd-derived"
              class="text-[11px] font-mono text-fg-faint text-left m-0 break-all"
            >
              ↳ {{ dlg.cwd.value }}
            </p>
            <p v-if="cwdError" class="text-xs text-red-500 m-0 text-left">
              {{ cwdError }}
            </p>
          </div>
```

Leave the surrounding empty-state intact: the `✦` icon, the "What would you like to build?" heading, and the `EXAMPLE_CHIPS` block immediately below remain.

- [ ] **Step 4: Remove the now-dead script symbols**

In the `<script setup>` block, delete the following — each is only used by the removed create path:

1. Imports (top of file, `src/components/RefinementChat.vue:1-12` region):
   - `import { fetchProjectFolders } from '../composables/useProjectFolders'`
   - `import { useProjects } from '../composables/useProjects'`
   - `import { useSpawnDialog } from '../composables/useSpawnDialog'`
   - `import { useSpawners } from '../composables/useSpawners'`
   - `import { createTask } from '../composables/useTasks'`
   - `import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'`
   - the `Project` type from the `import type { PipelineTask, Project } from '../types'` line → change to `import type { PipelineTask } from '../types'`

2. The `cwdError` ref (`src/components/RefinementChat.vue:46`):
   - `const cwdError = ref<string | null>(null)`

3. The project-picker block (`src/components/RefinementChat.vue:52-97`): the `const { projects } = useProjects()`, `const { spawners } = useSpawners()`, the `dlg = useSpawnDialog({...})` call, `projectChoice`, `showQuickCreate`, `sortedProjects`, `folderPickerVisible`, the `watch(projectChoice, ...)` block, `onProjectCreated`, and `onQuickCreateCancel`.

4. The `taskCreated` emit — check the `defineEmits` declaration. If it currently reads:
   ```ts
   const emit = defineEmits<{ close: [], confirmed: [task: PipelineTask], taskCreated: [task: PipelineTask] }>()
   ```
   change it to:
   ```ts
   const emit = defineEmits<{ close: [], confirmed: [task: PipelineTask] }>()
   ```

- [ ] **Step 5: Remove the now-unused `taskCreated` listener in App.vue**

`App.vue:354` currently has `@task-created="activeConceptTask = $event"` on the `<RefinementChat>` element. With the emit removed, delete that single attribute line. Leave `:open`, `:task`, `@close`, and `@confirmed` intact.

- [ ] **Step 6: Typecheck — this is the safety net for the manual deletions**

Run: `pnpm typecheck`
Expected: no errors. TypeScript + vue-tsc will flag any symbol you missed (e.g. a leftover `justCreatedId` is still used by the open/id watcher at `:220-237` — KEEP it; it is not part of the create path). If vue-tsc reports an unused import or an undefined `dlg`/`projectChoice`/`cwdError` in the template, you removed too little or too much — reconcile against Steps 3-4.

- [ ] **Step 7: Run the RefinementChat + App test suites**

Run: `pnpm test -- RefinementChat`
Expected: PASS — only the remaining RefinementChat tests (the two create-flow/cwd files are deleted). If a remaining test references `cwd-project-select`, `cwd-derived`, or `taskCreated`, it belonged to the removed path — delete that spec.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor(refine): drop dead standalone-create path; chat always opens on a task"
```

---

## Task 5: Verify backlog-stage refinement + full green

**Files:** none (verification only). This resolves the open risk flagged in the spec: RefinementChat now opens on a **backlog**-stage task, where it previously assumed `concept`.

- [ ] **Step 1: Inspect the confirm/promote flow for a stage assumption**

Run: `grep -rn "concept\|currentStage\|stage" src/composables/useRefinementChat.ts`
Then read `confirm()` in `src/composables/useRefinementChat.ts` and trace the endpoint it calls. Determine whether promotion to the next stage is driven server-side (by task id, stage-agnostic) or assumes the task is already at `concept`.

- [ ] **Step 2: Decide based on what you found**

- If `confirm()` posts to a refine endpoint keyed only by task id and the server advances the stage regardless of starting stage → no code change needed; record the finding in the commit message of Step 4.
- If the flow assumes `concept` (e.g. it reads `task.currentStage === 'concept'` or only promotes from concept) → the task must be at `concept` before the chat drives it. In that case, change `onCreateTaskAndRefine` in `src/App.vue` to promote first. Minimal options, in order of preference:
  - Pass `stage: 'concept'` on the Create & Refine path only: add a `createdAndRefine` variant in `BacklogForm.vue` that calls `createTask` with `stage: 'concept'`. (Cleanest: the form already has both emit handlers — give `onCreateAndRefine` its own `buildTask({ stage: 'concept' })`.)
  - Do NOT silently leave a backlog task that the chat cannot advance.

  If you take the `stage: 'concept'` route, update the BacklogForm test `emits createdAndRefine with the new task via Create & Refine` to assert `createTask` was called with `stage: 'concept'`, and re-run `pnpm test -- src/components/BacklogForm.test.ts`.

- [ ] **Step 3: Manual smoke test (dev server)**

Start the app (`task dev`), open the pipeline view, and verify:
1. Only one `+ New Task` button is present (no `+ Backlog`).
2. `+ New Task` → single-screen form. Selecting a project auto-fills the working directory. Title auto-derives the slug.
3. **Create** → task appears in the BACKLOG column; the task modal/selection opens.
4. **Create & Refine** → the refinement chat opens on the new task and accepts a first message (the agent streams a response).

If the dev server is not available in this environment, note that Step 3 is deferred to manual QA and rely on Steps 1-2 + the unit suite.

- [ ] **Step 4: Final full verification**

Run: `pnpm test`
Expected: PASS.

Run: `pnpm typecheck`
Expected: no errors.

Run: `pnpm build`
Expected: success (no unresolved imports from the deleted `DetailsStep.vue` / removed RefinementChat symbols).

- [ ] **Step 5: Commit (if Step 2 required a code change; otherwise record the verification finding)**

```bash
git add -A
git commit -m "fix(pipeline): ensure Create & Refine lands a refinable task"
```

If no code change was needed, skip the commit — the verification finding is captured in this plan.

---

## Self-Review Notes

- **Spec coverage:** Decision 1 (single button) → Task 3. Decision 2 (single screen) → Task 1 + Task 2. Decision 3 (single backlog stage) → Task 1 omits `stage`; verified in Task 5. Decision 4 (two actions) → Task 1 emits + Task 3 handlers. Decision 5 (remove standalone create) → Task 4. Spec "verify backlog→concept promotion" → Task 5.
- **Type consistency:** emit names `created` / `createdAndRefine` (kebab listener `@created-and-refine`) are used consistently across BacklogForm, its test, and App.vue. `selectTask`, `activeConceptTask`, `showRefinementChat`, `showBacklogForm` all already exist in App.vue.
- **Test-data note:** the rewritten BacklogForm test expects slug `demo-task` from title `Demo task` — relies on `slugify` lowercasing + hyphenating. If `slugify` differs, adjust the asserted slug to match `slugify('Demo task')`.
