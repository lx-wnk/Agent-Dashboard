<script setup lang="ts">
import type { PipelineTask, Project, ProjectFolder } from '@/types'
import { computed, ref, watch } from 'vue'
import PermissionTemplatePicker from '@/components/PermissionTemplatePicker.vue'
import QuickCreateProjectPanel from '@/components/QuickCreateProjectPanel.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppFieldLabel from '@/components/ui/AppFieldLabel.vue'
import AppInput from '@/components/ui/AppInput.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { suggestFolders } from '@/composables/useProjectFolders'
import { useProjects } from '@/composables/useProjects'
import { useSpawners } from '@/composables/useSpawners'
import { toast } from '@/composables/useToast'
import { useTrackerImport } from '@/composables/useTrackerImport'
import { createTask } from '@/features/pipeline/composables/useTasks'
import { errorMessage } from '@/utils/errorMessage'
import { slugify } from '@/utils/validation'

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

const priorityOptions: Array<{ value: string, label: string }> = [
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
]

const autonomyOptions: Array<{ value: string, label: string }> = [
  { value: 'manual', label: 'Manual — approve every stage' },
  { value: 'spec_gated', label: 'Spec-gated — approve the spec, then autonomous' },
  { value: 'full', label: 'Full — fully autonomous' },
]

const sortedProjects = computed(() =>
  projects.value.slice().sort((a, b) => a.name.localeCompare(b.name)),
)

const projectOptions = computed(() => [
  ...sortedProjects.value.map(p => ({ value: p.id, label: p.name })),
  { value: '__create__', label: '+ Create new project…' },
])

const spawnerOptions = computed(() => [
  { value: '', label: projectChoice.value && projectChoice.value !== '__create__' ? 'Project default' : 'Claude default' },
  ...spawners.value.map(s => ({ value: s.id, label: `${s.name}${s.builtIn ? ' (built-in)' : ''}` })),
])

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
      <AppSelect
        id="backlog-project"
        :model-value="projectChoice"
        :options="projectOptions"
        data-testid="backlog-project-select"
        class="w-full"
        @update:model-value="projectChoice = $event as string"
      />
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
        <AppSelect
          id="details-priority"
          :model-value="priority"
          :options="priorityOptions"
          class="w-full"
          @update:model-value="priority = $event as 'high' | 'medium' | 'low'"
        />
      </div>

      <div class="flex flex-col gap-1">
        <AppFieldLabel for="details-spawner">
          Spawner
        </AppFieldLabel>
        <AppSelect
          id="details-spawner"
          :model-value="selectedSpawnerId"
          :options="spawnerOptions"
          class="w-full"
          @update:model-value="selectedSpawnerId = $event as string"
        />
      </div>
    </div>

    <div class="flex flex-col gap-1">
      <AppFieldLabel for="details-autonomy">
        Autonomy
      </AppFieldLabel>
      <AppSelect
        id="details-autonomy"
        :model-value="autonomy"
        :options="autonomyOptions"
        data-testid="details-autonomy"
        class="w-full"
        @update:model-value="autonomy = $event as 'manual' | 'spec_gated' | 'full'"
      />
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
