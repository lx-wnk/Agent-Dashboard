<script setup lang="ts">
import type { PipelineTask, Project, ProjectFolder } from '../types'
import { computed, ref, watch } from 'vue'
import { suggestFolders } from '../composables/useProjectFolders'
import { useProjects } from '../composables/useProjects'
import { useSpawners } from '../composables/useSpawners'
import { createTask } from '../composables/useTasks'
import { errorMessage } from '../utils/errorMessage'
import { slugify } from '../utils/validation'
import PermissionTemplatePicker from './PermissionTemplatePicker.vue'
import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'

const emit = defineEmits<{
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

const fieldClass = 'w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent'

const sortedProjects = computed(() =>
  projects.value.slice().sort((a, b) => a.name.localeCompare(b.name)),
)

const canSubmit = computed(() =>
  !!title.value.trim()
  && !!slug.value.trim()
  && !!cwd.value.trim()
  && !!projectChoice.value
  && projectChoice.value !== '__create__'
  && !isSubmitting.value,
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
    if (def && !cwd.value.trim())
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
  if (!t || !s) {
    errorMsg.value = 'Title and slug are required, and a project must be selected.'
    return null
  }
  if (!c) {
    errorMsg.value = 'Selected project has no folder configured.'
    return null
  }
  isSubmitting.value = true
  errorMsg.value = ''
  try {
    const projectId = projectChoice.value
    return await createTask({
      slug: s,
      title: t,
      description: description.value.trim() || undefined,
      cwd: c,
      priority: priority.value,
      template: selectedTemplate.value ?? undefined,
      projectId,
      ...(selectedSpawnerId.value ? { spawnerId: selectedSpawnerId.value } : {}),
    })
  }
  catch (err: unknown) {
    errorMsg.value = errorMessage(err, 'Failed to create task')
    return null
  }
  finally {
    isSubmitting.value = false
  }
}

async function onCreateAndRefine(): Promise<void> {
  const task = await buildTask()
  if (task)
    emit('createdAndRefine', task)
}
</script>

<template>
  <form data-testid="backlog-form" class="space-y-4" @submit.prevent="onCreateAndRefine">
    <div class="flex flex-col gap-1">
      <label for="backlog-project" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Project</label>
      <select
        id="backlog-project"
        v-model="projectChoice"
        data-testid="backlog-project-select"
        :class="fieldClass"
      >
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
      <AppInput
        id="details-title"
        data-testid="details-title"
        :model-value="title"
        placeholder="What should the agent do?"
        @input="onTitleInput"
      />
    </div>

    <div class="flex flex-col gap-1">
      <label for="details-slug" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Slug</label>
      <AppInput
        id="details-slug"
        data-testid="details-slug"
        :model-value="slug"
        placeholder="task-slug"
        class="font-mono"
        @input="onSlugInput"
      />
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

    <p v-if="errorMsg" class="text-xs text-danger-text">
      {{ errorMsg }}
    </p>

    <div class="flex justify-end">
      <AppButton
        variant="primary"
        :disabled="!canSubmit"
        :aria-busy="isSubmitting"
        data-testid="details-submit-refine"
        @click="onCreateAndRefine"
      >
        {{ isSubmitting ? 'Creating…' : 'Create & Refine' }}
      </AppButton>
    </div>
  </form>
</template>
