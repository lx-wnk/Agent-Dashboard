# Task Creation Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-form `BacklogForm.vue` with a two-step wizard whose Step 1 picks or creates a project (with a skip path) and whose Step 2 collects the remaining task fields.

**Architecture:** `BacklogForm.vue` becomes a thin shell that owns shared state (`selectedProjectId`, `step`, prefill defaults) and renders either `ProjectStep.vue` or `DetailsStep.vue`. The two child components are presentation-only and emit step transitions / changes upward. The HTTP payload sent to `createTask` is unchanged, so the parent (`PipelineView`) needs no edits.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Vitest + `@vue/test-utils` for unit tests, Playwright for E2E, Tailwind CSS for styling.

---

## File Structure

| File | Responsibility |
|---|---|
| `src/components/BacklogForm.vue` (modify) | Wizard shell — owns shared state, switches between steps, submits to `createTask` |
| `src/components/backlog/ProjectStep.vue` (create) | Lists projects as radio cards, expands `QuickCreateProjectPanel`, exposes skip button |
| `src/components/backlog/DetailsStep.vue` (create) | Renders title/slug/description/cwd/priority/spawner/permission fields |
| `src/components/BacklogForm.test.ts` (create) | Vitest covering Step 1 ↔ Step 2 flow, prefill, skip path |
| `tests/e2e/backlog-form.spec.ts` (modify or create) | Playwright covering full create flow via the wizard |

## Spec Section A coverage map

| Spec bullet | Task |
|---|---|
| Wizard shell + shared state | Task 1 |
| ProjectStep: list existing | Task 2 |
| ProjectStep: create new inline | Task 3 |
| ProjectStep: skip path | Task 4 |
| DetailsStep: prefilled cwd from project default folder | Task 5 |
| DetailsStep: spawner override default | Task 5 |
| Back preserves state | Task 6 |
| Submit identical payload | Task 7 |
| E2E coverage | Task 8 |

---

### Task 1: Wizard shell with shared state

**Files:**
- Modify: `src/components/BacklogForm.vue`
- Create: `src/components/backlog/ProjectStep.vue` (placeholder)
- Create: `src/components/backlog/DetailsStep.vue` (placeholder)

- [ ] **Step 1: Create empty ProjectStep placeholder**

Write `src/components/backlog/ProjectStep.vue`:

```vue
<script setup lang="ts">
import type { Project } from '../../types'

defineProps<{
  projects: Project[]
  selectedProjectId: string
  skipped: boolean
}>()

defineEmits<{
  'update:selectedProjectId': [id: string]
  'update:skipped': [skipped: boolean]
  next: []
}>()
</script>

<template>
  <div data-testid="project-step">
    <p class="text-fg-mute">Project selection placeholder</p>
  </div>
</template>
```

- [ ] **Step 2: Create empty DetailsStep placeholder**

Write `src/components/backlog/DetailsStep.vue`:

```vue
<script setup lang="ts">
defineProps<{
  selectedProjectId: string
  skipped: boolean
}>()

defineEmits<{
  back: []
  created: []
}>()
</script>

<template>
  <div data-testid="details-step">
    <p class="text-fg-mute">Details placeholder</p>
  </div>
</template>
```

- [ ] **Step 3: Replace BacklogForm.vue with wizard shell**

Replace the whole file with:

```vue
<script setup lang="ts">
import type { PipelineTask } from '../types'
import { ref } from 'vue'
import { useProjects } from '../composables/useProjects'
import ProjectStep from './backlog/ProjectStep.vue'
import DetailsStep from './backlog/DetailsStep.vue'

const emit = defineEmits<{ created: [task: PipelineTask] }>()

const { projects } = useProjects()
const step = ref<1 | 2>(1)
const selectedProjectId = ref<string>('')
const skipped = ref(false)

function onNext(): void {
  step.value = 2
}

function onBack(): void {
  step.value = 1
}

function onCreated(task: PipelineTask): void {
  emit('created', task)
  step.value = 1
  selectedProjectId.value = ''
  skipped.value = false
}
</script>

<template>
  <div>
    <ProjectStep
      v-if="step === 1"
      :projects="projects"
      :selected-project-id="selectedProjectId"
      :skipped="skipped"
      @update:selected-project-id="selectedProjectId = $event"
      @update:skipped="skipped = $event"
      @next="onNext"
    />
    <DetailsStep
      v-else
      :selected-project-id="selectedProjectId"
      :skipped="skipped"
      @back="onBack"
      @created="onCreated"
    />
  </div>
</template>
```

- [ ] **Step 4: Run typecheck**

Run: `pnpm typecheck`
Expected: PASS — all types satisfied.

- [ ] **Step 5: Commit**

```bash
git add src/components/BacklogForm.vue src/components/backlog/ProjectStep.vue src/components/backlog/DetailsStep.vue
git commit -m "refactor(backlog): split BacklogForm into two-step wizard shell"
```

---

### Task 2: ProjectStep — list existing projects + Next

**Files:**
- Modify: `src/components/backlog/ProjectStep.vue`
- Create: `src/components/BacklogForm.test.ts`

- [ ] **Step 1: Write failing test for project list rendering**

Write `src/components/BacklogForm.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('../composables/useProjects', () => ({
  useProjects: () => ({
    projects: { value: [
      { id: 'p1', slug: 'web', name: 'Web', folders: [{ id: 'f1', projectId: 'p1', path: '/repos/web', isDefault: true, createdAt: '' }], createdAt: '', updatedAt: '' },
      { id: 'p2', slug: 'api', name: 'API', folders: [], createdAt: '', updatedAt: '' },
    ] },
  }),
}))

import BacklogForm from './BacklogForm.vue'

describe('BacklogForm wizard', () => {
  it('renders one radio per project in Step 1', () => {
    const wrapper = mount(BacklogForm)
    const radios = wrapper.findAll('[data-testid^="project-radio-"]')
    expect(radios).toHaveLength(2)
    expect(radios[0].text()).toContain('Web')
    expect(radios[1].text()).toContain('API')
  })

  it('disables Next until a project is selected or skipped', async () => {
    const wrapper = mount(BacklogForm)
    const next = wrapper.get('[data-testid="project-step-next"]')
    expect(next.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    expect(next.attributes('disabled')).toBeUndefined()
  })

  it('advances to Step 2 when Next is clicked', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    await wrapper.get('[data-testid="project-step-next"]').trigger('click')
    expect(wrapper.find('[data-testid="details-step"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-step"]').exists()).toBe(false)
  })
})
```

- [ ] **Step 2: Run test and confirm it fails**

Run: `pnpm test BacklogForm.test.ts`
Expected: FAIL — `project-radio-*` and `project-step-next` selectors missing.

- [ ] **Step 3: Implement ProjectStep list + Next**

Replace `src/components/backlog/ProjectStep.vue` with:

```vue
<script setup lang="ts">
import type { Project } from '../../types'
import { computed } from 'vue'
import AppButton from '../ui/AppButton.vue'

const props = defineProps<{
  projects: Project[]
  selectedProjectId: string
  skipped: boolean
}>()

const emit = defineEmits<{
  'update:selectedProjectId': [id: string]
  'update:skipped': [skipped: boolean]
  next: []
}>()

const canAdvance = computed(() => !!props.selectedProjectId || props.skipped)

function selectProject(id: string): void {
  emit('update:selectedProjectId', id)
  emit('update:skipped', false)
}
</script>

<template>
  <div data-testid="project-step" class="space-y-4">
    <h2 class="text-sm font-semibold uppercase tracking-wider text-fg-mute">
      Step 1 — Project
    </h2>

    <div class="space-y-2">
      <button
        v-for="p in projects"
        :key="p.id"
        type="button"
        :data-testid="`project-radio-${p.id}`"
        class="w-full text-left bg-app border rounded p-3 transition"
        :class="selectedProjectId === p.id ? 'border-blue-500' : 'border-line hover:border-blue-300'"
        @click="selectProject(p.id)"
      >
        <div class="text-fg font-medium">{{ p.name }}</div>
        <div class="text-xs text-fg-mute">{{ p.folders?.[0]?.path ?? '—' }}</div>
      </button>
    </div>

    <div class="flex justify-end">
      <AppButton
        variant="primary"
        :disabled="!canAdvance"
        data-testid="project-step-next"
        @click="emit('next')"
      >
        Next
      </AppButton>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Run test and confirm it passes**

Run: `pnpm test BacklogForm.test.ts`
Expected: PASS — all three test cases green.

- [ ] **Step 5: Commit**

```bash
git add src/components/backlog/ProjectStep.vue src/components/BacklogForm.test.ts
git commit -m "feat(backlog): list projects and gate Next in wizard Step 1"
```

---

### Task 3: ProjectStep — inline create-new via QuickCreateProjectPanel

**Files:**
- Modify: `src/components/backlog/ProjectStep.vue`
- Modify: `src/components/BacklogForm.test.ts`

- [ ] **Step 1: Write failing test for create-new flow**

Append to `src/components/BacklogForm.test.ts` inside the existing `describe`:

```ts
it('expands QuickCreateProjectPanel when "Create new" is clicked', async () => {
  const wrapper = mount(BacklogForm)
  expect(wrapper.find('[data-testid="quick-create-panel"]').exists()).toBe(false)
  await wrapper.get('[data-testid="project-step-create-new"]').trigger('click')
  expect(wrapper.find('[data-testid="quick-create-panel"]').exists()).toBe(true)
})

it('selects the newly created project after QuickCreateProjectPanel emits created', async () => {
  const wrapper = mount(BacklogForm)
  await wrapper.get('[data-testid="project-step-create-new"]').trigger('click')
  const panel = wrapper.findComponent({ name: 'QuickCreateProjectPanel' })
  panel.vm.$emit('created', {
    id: 'p3', slug: 'new', name: 'Brand New', folders: [{ id: 'f3', projectId: 'p3', path: '/x', isDefault: true, createdAt: '' }], createdAt: '', updatedAt: '',
  })
  await wrapper.vm.$nextTick()
  const next = wrapper.get('[data-testid="project-step-next"]')
  expect(next.attributes('disabled')).toBeUndefined()
})
```

The `QuickCreateProjectPanel` needs `data-testid="quick-create-panel"` on its root or to be findable by component name. We will use the component-name path; we also need `useSpawners` mocked so the panel renders.

Add at the top of the test file (next to the `useProjects` mock):

```ts
vi.mock('../composables/useSpawners', () => ({
  useSpawners: () => ({ spawners: { value: [{ id: 's1', name: 'Claude default', slug: 'claude-default', command: 'claude', args: [], env: {}, adapterType: 'claude', adapterConfig: {}, builtIn: true, createdAt: '', updatedAt: '' }] } }),
}))
```

- [ ] **Step 2: Run test and confirm it fails**

Run: `pnpm test BacklogForm.test.ts`
Expected: FAIL — `project-step-create-new` selector missing.

- [ ] **Step 3: Add Create-new button + QuickCreateProjectPanel**

In `src/components/backlog/ProjectStep.vue`, add an import, ref, and template block:

```vue
<script setup lang="ts">
import type { Project } from '../../types'
import { computed, ref } from 'vue'
import { useSpawners } from '../../composables/useSpawners'
import AppButton from '../ui/AppButton.vue'
import QuickCreateProjectPanel from '../QuickCreateProjectPanel.vue'

const props = defineProps<{
  projects: Project[]
  selectedProjectId: string
  skipped: boolean
}>()

const emit = defineEmits<{
  'update:selectedProjectId': [id: string]
  'update:skipped': [skipped: boolean]
  'project-created': [project: Project]
  next: []
}>()

const { spawners } = useSpawners()
const showCreate = ref(false)

const canAdvance = computed(() => !!props.selectedProjectId || props.skipped)

function selectProject(id: string): void {
  emit('update:selectedProjectId', id)
  emit('update:skipped', false)
}

function onCreated(project: Project): void {
  showCreate.value = false
  emit('project-created', project)
  emit('update:selectedProjectId', project.id)
  emit('update:skipped', false)
}
</script>

<template>
  <div data-testid="project-step" class="space-y-4">
    <h2 class="text-sm font-semibold uppercase tracking-wider text-fg-mute">
      Step 1 — Project
    </h2>

    <div class="space-y-2">
      <button
        v-for="p in projects"
        :key="p.id"
        type="button"
        :data-testid="`project-radio-${p.id}`"
        class="w-full text-left bg-app border rounded p-3 transition"
        :class="selectedProjectId === p.id ? 'border-blue-500' : 'border-line hover:border-blue-300'"
        @click="selectProject(p.id)"
      >
        <div class="text-fg font-medium">{{ p.name }}</div>
        <div class="text-xs text-fg-mute">{{ p.folders?.[0]?.path ?? '—' }}</div>
      </button>
    </div>

    <button
      v-if="!showCreate"
      type="button"
      data-testid="project-step-create-new"
      class="text-xs text-blue-500 hover:underline"
      @click="showCreate = true"
    >
      + Create new project
    </button>

    <div v-if="showCreate" data-testid="quick-create-panel">
      <QuickCreateProjectPanel
        :spawners="spawners"
        @created="onCreated"
        @cancel="showCreate = false"
      />
    </div>

    <div class="flex justify-end">
      <AppButton
        variant="primary"
        :disabled="!canAdvance"
        data-testid="project-step-next"
        @click="emit('next')"
      >
        Next
      </AppButton>
    </div>
  </div>
</template>
```

In `src/components/BacklogForm.vue`, update the wizard shell to consume the new event and refresh `projects`:

```vue
<script setup lang="ts">
import type { PipelineTask, Project } from '../types'
import { ref } from 'vue'
import { useProjects } from '../composables/useProjects'
import ProjectStep from './backlog/ProjectStep.vue'
import DetailsStep from './backlog/DetailsStep.vue'

const emit = defineEmits<{ created: [task: PipelineTask] }>()

const { projects, refresh } = useProjects()
const step = ref<1 | 2>(1)
const selectedProjectId = ref<string>('')
const skipped = ref(false)

function onNext(): void {
  step.value = 2
}

function onBack(): void {
  step.value = 1
}

function onCreated(task: PipelineTask): void {
  emit('created', task)
  step.value = 1
  selectedProjectId.value = ''
  skipped.value = false
}

function onProjectCreated(_project: Project): void {
  void refresh?.()
}
</script>

<template>
  <div>
    <ProjectStep
      v-if="step === 1"
      :projects="projects"
      :selected-project-id="selectedProjectId"
      :skipped="skipped"
      @update:selected-project-id="selectedProjectId = $event"
      @update:skipped="skipped = $event"
      @project-created="onProjectCreated"
      @next="onNext"
    />
    <DetailsStep
      v-else
      :selected-project-id="selectedProjectId"
      :skipped="skipped"
      @back="onBack"
      @created="onCreated"
    />
  </div>
</template>
```

If `useProjects()` does not expose `refresh`, leave the `onProjectCreated` body empty (the newly emitted project from the panel is already merged into the local list by `useProjects` when it implements optimistic updates; check `src/composables/useProjects.ts` and adapt the call to whatever refresh function it actually exposes — common names: `refresh`, `reload`, `refetch`).

- [ ] **Step 4: Run test and confirm it passes**

Run: `pnpm test BacklogForm.test.ts`
Expected: PASS — both new test cases green; existing cases still green.

- [ ] **Step 5: Commit**

```bash
git add src/components/backlog/ProjectStep.vue src/components/BacklogForm.vue src/components/BacklogForm.test.ts
git commit -m "feat(backlog): inline QuickCreateProjectPanel in wizard Step 1"
```

---

### Task 4: ProjectStep — skip path

**Files:**
- Modify: `src/components/backlog/ProjectStep.vue`
- Modify: `src/components/BacklogForm.test.ts`

- [ ] **Step 1: Write failing test for skip**

Append to the existing `describe` in `src/components/BacklogForm.test.ts`:

```ts
it('skip button enables Next and clears project selection', async () => {
  const wrapper = mount(BacklogForm)
  await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
  await wrapper.get('[data-testid="project-step-skip"]').trigger('click')
  expect(wrapper.get('[data-testid="project-step-next"]').attributes('disabled')).toBeUndefined()
  // After skipping, the previously selected radio should no longer be styled as selected.
  expect(wrapper.get('[data-testid="project-radio-p1"]').classes()).not.toContain('border-blue-500')
})
```

- [ ] **Step 2: Run test and confirm it fails**

Run: `pnpm test BacklogForm.test.ts`
Expected: FAIL — `project-step-skip` selector missing.

- [ ] **Step 3: Add Skip button**

Modify `src/components/backlog/ProjectStep.vue`'s template `flex justify-end` block:

```vue
<div class="flex justify-end gap-2">
  <AppButton
    variant="secondary"
    data-testid="project-step-skip"
    @click="onSkip"
  >
    Skip — no project
  </AppButton>
  <AppButton
    variant="primary"
    :disabled="!canAdvance"
    data-testid="project-step-next"
    @click="emit('next')"
  >
    Next
  </AppButton>
</div>
```

Add `onSkip` to the `<script setup>`:

```ts
function onSkip(): void {
  emit('update:selectedProjectId', '')
  emit('update:skipped', true)
}
```

- [ ] **Step 4: Run test and confirm it passes**

Run: `pnpm test BacklogForm.test.ts`
Expected: PASS — skip test green; all previous tests still green.

- [ ] **Step 5: Commit**

```bash
git add src/components/backlog/ProjectStep.vue src/components/BacklogForm.test.ts
git commit -m "feat(backlog): add skip path to wizard Step 1"
```

---

### Task 5: DetailsStep — fields + prefill from selected project

**Files:**
- Modify: `src/components/backlog/DetailsStep.vue`
- Modify: `src/components/BacklogForm.test.ts`

- [ ] **Step 1: Write failing tests for prefill and submit payload**

Append to the existing `describe` in `src/components/BacklogForm.test.ts`. First add module-level mocks for the network helpers next to the existing mocks:

```ts
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
```

Then add test cases:

```ts
it('prefills cwd from the project default folder when the user advances to Step 2', async () => {
  const wrapper = mount(BacklogForm)
  await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
  await wrapper.get('[data-testid="project-step-next"]').trigger('click')
  await flushPromises()
  const cwd = wrapper.get('[data-testid="details-cwd"]').element as HTMLInputElement
  expect(cwd.value).toBe('/repos/web')
})

it('submits createTask with projectId when project is selected', async () => {
  const wrapper = mount(BacklogForm)
  await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
  await wrapper.get('[data-testid="project-step-next"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
  await wrapper.get('[data-testid="details-slug"]').setValue('demo')
  await wrapper.get('[data-testid="details-submit"]').trigger('click')
  await flushPromises()
  expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({
    projectId: 'p1',
    title: 'Demo task',
    slug: 'demo',
    cwd: '/repos/web',
  }))
})

it('submits createTask without projectId when Step 1 is skipped', async () => {
  createTaskMock.mockClear()
  const wrapper = mount(BacklogForm)
  await wrapper.get('[data-testid="project-step-skip"]').trigger('click')
  await wrapper.get('[data-testid="project-step-next"]').trigger('click')
  await wrapper.get('[data-testid="details-title"]').setValue('No project')
  await wrapper.get('[data-testid="details-slug"]').setValue('no-project')
  await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/x')
  await wrapper.get('[data-testid="details-submit"]').trigger('click')
  await flushPromises()
  expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ projectId: expect.anything() }))
})
```

Also add `import { flushPromises } from '@vue/test-utils'` to the import block at the top.

- [ ] **Step 2: Run test and confirm it fails**

Run: `pnpm test BacklogForm.test.ts`
Expected: FAIL — `details-*` selectors missing.

- [ ] **Step 3: Implement DetailsStep**

Replace `src/components/backlog/DetailsStep.vue` with:

```vue
<script setup lang="ts">
import type { ProjectFolder } from '../../types'
import { onMounted, ref } from 'vue'
import { suggestFolders } from '../../composables/useProjectFolders'
import { createTask } from '../../composables/useTasks'
import { useSpawners } from '../../composables/useSpawners'
import { slugify } from '../../utils/validation'
import PermissionTemplatePicker from '../PermissionTemplatePicker.vue'
import AppButton from '../ui/AppButton.vue'
import AppInput from '../ui/AppInput.vue'

const props = defineProps<{
  selectedProjectId: string
  skipped: boolean
}>()

const emit = defineEmits<{
  back: []
  created: [task: unknown]
}>()

type PermissionTemplateId = 'research_only' | 'test_only' | 'review_only' | 'feature_implementation'

const title = ref('')
const slug = ref('')
const description = ref('')
const cwd = ref('')
const priority = ref<'high' | 'medium' | 'low'>('medium')
const selectedTemplate = ref<PermissionTemplateId | null>('feature_implementation')
const { spawners } = useSpawners()
const selectedSpawnerId = ref<string>('')
const folderSuggestions = ref<ProjectFolder[]>([])
const isSubmitting = ref(false)
const errorMsg = ref('')

function onTitleInput(value: string): void {
  title.value = value
  if (!slug.value || slug.value === slugify(title.value.slice(0, -1)))
    slug.value = slugify(value)
}

onMounted(async () => {
  if (!props.selectedProjectId)
    return
  try {
    const suggestions = await suggestFolders(props.selectedProjectId)
    folderSuggestions.value = suggestions
    const def = suggestions.find(f => f.isDefault) ?? suggestions[0]
    if (def && !cwd.value.trim())
      cwd.value = def.path
  }
  catch {
    // free-text fallback still available
  }
})

async function handleSubmit(): Promise<void> {
  if (isSubmitting.value)
    return
  const t = title.value.trim()
  const s = slug.value.trim()
  const c = cwd.value.trim()
  if (!t || !s || !c) {
    errorMsg.value = 'Title, slug, and working directory are required.'
    return
  }
  isSubmitting.value = true
  errorMsg.value = ''
  try {
    const task = await createTask({
      slug: s,
      title: t,
      description: description.value.trim() || undefined,
      cwd: c,
      priority: priority.value,
      template: selectedTemplate.value ?? undefined,
      ...(props.selectedProjectId ? { projectId: props.selectedProjectId } : {}),
      ...(selectedSpawnerId.value ? { spawnerId: selectedSpawnerId.value } : {}),
    })
    emit('created', task)
  }
  catch (err: unknown) {
    errorMsg.value = err instanceof Error ? err.message : 'Failed to create task'
  }
  finally {
    isSubmitting.value = false
  }
}
</script>

<template>
  <form data-testid="details-step" class="space-y-4" @submit.prevent="handleSubmit">
    <div class="flex items-center justify-between">
      <h2 class="text-sm font-semibold uppercase tracking-wider text-fg-mute">Step 2 — Task</h2>
      <button type="button" class="text-xs text-fg-mute hover:underline" data-testid="details-back" @click="emit('back')">← Back</button>
    </div>

    <AppInput data-testid="details-title" :model-value="title" placeholder="What should the agent do?" @update:model-value="onTitleInput" />
    <AppInput data-testid="details-slug" v-model="slug" placeholder="task-slug" />

    <input
      data-testid="details-cwd"
      v-model="cwd"
      list="details-cwd-list"
      placeholder="/path/to/project"
      class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2"
    >
    <datalist id="details-cwd-list">
      <option v-for="folder in folderSuggestions" :key="folder.id" :value="folder.path">{{ folder.label || folder.path }}</option>
    </datalist>

    <AppInput v-model="description" type="textarea" :rows="3" placeholder="Additional context (optional)" />

    <select v-model="priority" class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2">
      <option value="high">High</option>
      <option value="medium">Medium</option>
      <option value="low">Low</option>
    </select>

    <select v-model="selectedSpawnerId" class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2">
      <option value="">{{ selectedProjectId ? 'Project default' : 'Claude default' }}</option>
      <option v-for="s in spawners" :key="s.id" :value="s.id">{{ s.name }}{{ s.builtIn ? ' (built-in)' : '' }}</option>
    </select>

    <PermissionTemplatePicker v-model="selectedTemplate" />

    <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400">{{ errorMsg }}</p>

    <div class="flex justify-end">
      <AppButton
        variant="primary"
        :disabled="isSubmitting || !title.trim() || !slug.trim() || !cwd.trim()"
        data-testid="details-submit"
        @click="handleSubmit"
      >
        {{ isSubmitting ? 'Creating…' : 'Create Task' }}
      </AppButton>
    </div>
  </form>
</template>
```

- [ ] **Step 4: Run test and confirm it passes**

Run: `pnpm test BacklogForm.test.ts`
Expected: PASS — all DetailsStep cases plus prior ones still green.

- [ ] **Step 5: Commit**

```bash
git add src/components/backlog/DetailsStep.vue src/components/BacklogForm.test.ts
git commit -m "feat(backlog): collect task fields and submit createTask in wizard Step 2"
```

---

### Task 6: Back preserves Step 2 state

**Files:**
- Modify: `src/components/BacklogForm.vue`
- Modify: `src/components/BacklogForm.test.ts`

- [ ] **Step 1: Write failing test**

Append:

```ts
it('preserves Step 2 field values after Back → Next round-trip', async () => {
  const wrapper = mount(BacklogForm)
  await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
  await wrapper.get('[data-testid="project-step-next"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="details-title"]').setValue('Persisted title')
  await wrapper.get('[data-testid="details-back"]').trigger('click')
  await wrapper.get('[data-testid="project-step-next"]').trigger('click')
  await flushPromises()
  const titleInput = wrapper.get('[data-testid="details-title"]').element as HTMLInputElement
  expect(titleInput.value).toBe('Persisted title')
})
```

- [ ] **Step 2: Run test and confirm it fails**

Run: `pnpm test BacklogForm.test.ts`
Expected: FAIL — Step 2 component is re-mounted on each visit, losing state.

- [ ] **Step 3: Hoist Step 2 state into BacklogForm**

Use `keep-alive` so the `DetailsStep` instance survives the toggle. Update `src/components/BacklogForm.vue` template:

```vue
<template>
  <div>
    <ProjectStep
      v-if="step === 1"
      :projects="projects"
      :selected-project-id="selectedProjectId"
      :skipped="skipped"
      @update:selected-project-id="selectedProjectId = $event"
      @update:skipped="skipped = $event"
      @project-created="onProjectCreated"
      @next="onNext"
    />
    <KeepAlive>
      <DetailsStep
        v-if="step === 2"
        :selected-project-id="selectedProjectId"
        :skipped="skipped"
        @back="onBack"
        @created="onCreated"
      />
    </KeepAlive>
  </div>
</template>
```

- [ ] **Step 4: Run test and confirm it passes**

Run: `pnpm test BacklogForm.test.ts`
Expected: PASS — Back/Next round-trip preserves title.

- [ ] **Step 5: Commit**

```bash
git add src/components/BacklogForm.vue src/components/BacklogForm.test.ts
git commit -m "feat(backlog): preserve Step 2 state across Back/Next via KeepAlive"
```

---

### Task 7: Typecheck + full unit test sweep

**Files:** none (regression check)

- [ ] **Step 1: Run vue-tsc**

Run: `pnpm typecheck`
Expected: PASS.

- [ ] **Step 2: Run all unit tests**

Run: `pnpm test`
Expected: PASS — entire Vitest suite green, no regressions.

- [ ] **Step 3: Fix anything that broke**

If a previously-green test relied on the old single-form `BacklogForm` structure (selectors like the original `#backlog-title`), update those tests to use the new `data-testid` selectors. Re-run `pnpm test` until green.

- [ ] **Step 4: Commit (only if fixes were needed)**

```bash
git add -A
git commit -m "test(backlog): update selectors for wizard refactor"
```

---

### Task 8: Playwright E2E covers wizard flow

**Files:**
- Create or modify: `tests/e2e/backlog-form.spec.ts`

- [ ] **Step 1: Confirm where backlog E2E currently lives**

Run: `ls tests/e2e | grep -i backlog`
If a file exists, modify it; if not, create `tests/e2e/backlog-form.spec.ts`.

- [ ] **Step 2: Write failing E2E**

Write `tests/e2e/backlog-form.spec.ts` (or extend the existing file with the test below):

```ts
import { expect, test } from '@playwright/test'

test('create task via two-step wizard with project selection', async ({ page }) => {
  await page.goto('/')

  // Seed: assume the dev fixture provides at least one project.
  await page.getByRole('button', { name: /new task|backlog/i }).click()

  // Step 1: pick first project.
  await page.locator('[data-testid^="project-radio-"]').first().click()
  await page.getByTestId('project-step-next').click()

  // Step 2: fill and submit.
  await page.getByTestId('details-title').fill('E2E wizard task')
  await page.getByTestId('details-slug').fill('e2e-wizard-task')
  // cwd should be prefilled — assert non-empty.
  const cwd = page.getByTestId('details-cwd')
  await expect(cwd).not.toHaveValue('')
  await page.getByTestId('details-submit').click()

  await expect(page.getByText('E2E wizard task')).toBeVisible()
})

test('create task via wizard with skip path', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: /new task|backlog/i }).click()
  await page.getByTestId('project-step-skip').click()
  await page.getByTestId('project-step-next').click()
  await page.getByTestId('details-title').fill('Skip path task')
  await page.getByTestId('details-slug').fill('skip-path-task')
  await page.getByTestId('details-cwd').fill('/tmp/skip-path-task')
  await page.getByTestId('details-submit').click()
  await expect(page.getByText('Skip path task')).toBeVisible()
})
```

If the actual control to open the backlog form has a different label, adjust the `getByRole` selector to match the real UI (check `src/App.vue` or `PipelineBoard.vue` for the trigger text).

- [ ] **Step 3: Run the new E2E**

Run: `pnpm test:e2e tests/e2e/backlog-form.spec.ts`
Expected: PASS — both cases green.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/backlog-form.spec.ts
git commit -m "test(e2e): cover backlog wizard project pick, create, and skip paths"
```

---

## Done Criteria

- `pnpm typecheck` passes.
- `pnpm test` passes including the new `BacklogForm.test.ts`.
- `pnpm test:e2e tests/e2e/backlog-form.spec.ts` passes.
- Visually: opening the backlog form shows Step 1 first; selecting or skipping a project advances to Step 2 with cwd prefilled (when a project was picked); Back/Next round-trip preserves field state; Submit creates a task identical to the pre-wizard payload.
