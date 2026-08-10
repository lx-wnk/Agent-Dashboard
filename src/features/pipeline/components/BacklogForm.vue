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
import { TASK_AUTONOMY_OPTIONS, TASK_PRIORITY_OPTIONS } from '@/utils/taskOptions'
import { slugFollowingName } from '@/utils/validation'

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
const slugTouched = ref(false)
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

// Watching the flag too is what hands the slug back when the field is emptied.
// Both sources flush together, after the input event's own v-model write, which
// runs before the handler on a native input and after it on AppInput.
watch([title, slugTouched], ([v, touched]) => {
  slug.value = slugFollowingName(v, slug.value, touched)
})

function onSlugInput(e: Event): void {
  slugTouched.value = (e.target as HTMLInputElement).value.length > 0
}

async function importFromIssue(): Promise<void> {
  const ref = importRef.value.trim()
  if (!ref || isImporting.value)
    return
  isImporting.value = true
  try {
    const iss = await fetchIssue(ref)
    slugTouched.value = false
    title.value = iss.title
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
        v-model="projectChoice"
        :options="projectOptions"
        data-testid="backlog-project-select"
        class="w-full"
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
        v-model="title"
        data-testid="details-title"
        placeholder="What should the agent do?"
      />
    </div>

    <div class="flex flex-col gap-1">
      <AppFieldLabel for="details-slug">
        Slug
      </AppFieldLabel>
      <AppInput
        id="details-slug"
        v-model="slug"
        data-testid="details-slug"
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
          v-model="priority"
          :options="TASK_PRIORITY_OPTIONS"
          class="w-full"
        />
      </div>

      <div class="flex flex-col gap-1">
        <AppFieldLabel for="details-spawner">
          Spawner
        </AppFieldLabel>
        <AppSelect
          id="details-spawner"
          v-model="selectedSpawnerId"
          :options="spawnerOptions"
          class="w-full"
        />
      </div>
    </div>

    <div class="flex flex-col gap-1">
      <AppFieldLabel for="details-autonomy">
        Autonomy
      </AppFieldLabel>
      <AppSelect
        id="details-autonomy"
        v-model="autonomy"
        :options="TASK_AUTONOMY_OPTIONS"
        data-testid="details-autonomy"
        class="w-full"
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
