<script setup lang="ts">
import type { PipelineTask, Project, ProjectFolder } from '../types'
import { computed, ref, watch } from 'vue'
import { suggestFolders } from '../composables/useProjectFolders'
import { useProjects } from '../composables/useProjects'
import { useSpawners } from '../composables/useSpawners'
import { createTask } from '../composables/useTasks'
import { toast } from '../composables/useToast'
import { useTrackerImport } from '../composables/useTrackerImport'
import { errorMessage } from '../utils/errorMessage'
import { slugify } from '../utils/validation'
import PermissionTemplatePicker from './PermissionTemplatePicker.vue'
import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'
import AppButton from './ui/AppButton.vue'
import AppFieldLabel from './ui/AppFieldLabel.vue'
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
const autonomy = ref<'manual' | 'spec_gated' | 'full'>('spec_gated')
const folderSuggestions = ref<ProjectFolder[]>([])
const isSubmitting = ref(false)
const importRef = ref('')
const isImporting = ref(false)
const { fetchIssue } = useTrackerImport()

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

async function importFromIssue(): Promise<void> {
  const ref = importRef.value.trim()
  if (!ref || isImporting.value)
    return
  isImporting.value = true
  try {
    const iss = await fetchIssue(ref)
    title.value = iss.title
    slug.value = slugify(iss.title)
    const sourceLink = `\n\nSource: ${iss.url}`
    description.value = iss.body ? iss.body + sourceLink : iss.url
  }
  catch (err: unknown) {
    toast.error(errorMessage(err, 'Failed to fetch issue'))
  }
  finally {
    isImporting.value = false
  }
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
    toast.error('Title and slug are required, and a project must be selected.')
    return null
  }
  if (!c) {
    toast.error('Selected project has no folder configured.')
    return null
  }
  isSubmitting.value = true
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
      autonomy: autonomy.value,
      ...(selectedSpawnerId.value ? { spawnerId: selectedSpawnerId.value } : {}),
    })
  }
  catch (err: unknown) {
    toast.error(errorMessage(err, 'Failed to create task'))
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
      <AppFieldLabel for="backlog-project">
        Project
      </AppFieldLabel>
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
      <AppFieldLabel>Import from issue</AppFieldLabel>
      <div class="flex gap-2 items-stretch">
        <AppInput
          v-model="importRef"
          placeholder="github.com/owner/repo/issues/1 · KEY-123 · owner/repo#1"
          class="flex-1"
          data-testid="import-ref-input"
        />
        <AppButton
          type="button"
          variant="secondary"
          :disabled="!importRef.trim() || isImporting"
          :aria-busy="isImporting"
          data-testid="import-ref-fetch"
          @click="importFromIssue"
        >
          {{ isImporting ? 'Fetching…' : 'Fetch' }}
        </AppButton>
      </div>
    </div>

    <div class="flex flex-col gap-1">
      <AppFieldLabel for="details-title">
        Title
      </AppFieldLabel>
      <AppInput
        id="details-title"
        data-testid="details-title"
        :model-value="title"
        placeholder="What should the agent do?"
        @input="onTitleInput"
      />
    </div>

    <div class="flex flex-col gap-1">
      <AppFieldLabel for="details-slug">
        Slug
      </AppFieldLabel>
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

    <div class="grid grid-cols-2 gap-3">
      <div class="flex flex-col gap-1">
        <AppFieldLabel for="details-priority">
          Priority
        </AppFieldLabel>
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
        <AppFieldLabel for="details-spawner">
          Spawner
        </AppFieldLabel>
        <select id="details-spawner" v-model="selectedSpawnerId" :class="fieldClass">
          <option value="">
            {{ projectChoice && projectChoice !== '__create__' ? 'Project default' : 'Claude default' }}
          </option>
          <option v-for="s in spawners" :key="s.id" :value="s.id">
            {{ s.name }}{{ s.builtIn ? ' (built-in)' : '' }}
          </option>
        </select>
      </div>
    </div>

    <div class="flex flex-col gap-1">
      <AppFieldLabel for="details-autonomy">
        Autonomy
      </AppFieldLabel>
      <select
        id="details-autonomy"
        v-model="autonomy"
        data-testid="details-autonomy"
        :class="fieldClass"
      >
        <option value="manual">
          Manual — approve every stage
        </option>
        <option value="spec_gated">
          Spec-gated — approve the spec, then autonomous
        </option>
        <option value="full">
          Full — fully autonomous
        </option>
      </select>
    </div>

    <PermissionTemplatePicker v-model="selectedTemplate" />

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
