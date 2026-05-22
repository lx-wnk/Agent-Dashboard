<script setup lang="ts">
import type { PipelineTask, ProjectFolder } from '../../types'
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
  created: [task: PipelineTask]
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

const fieldClass = 'w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-blue-500'

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
      <h2 class="text-sm font-semibold uppercase tracking-wider text-fg-mute">
        Step 2 — Task
      </h2>
      <button type="button" class="text-xs text-fg-mute hover:underline" data-testid="details-back" @click="emit('back')">
        ← Back
      </button>
    </div>

    <input
      data-testid="details-title"
      :value="title"
      placeholder="What should the agent do?"
      :class="fieldClass"
      @input="onTitleInput"
    >

    <input
      data-testid="details-slug"
      :value="slug"
      placeholder="task-slug"
      :class="fieldClass"
      @input="onSlugInput"
    >

    <input
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

    <AppInput v-model="description" type="textarea" :rows="3" placeholder="Additional context (optional)" />

    <select v-model="priority" :class="fieldClass">
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

    <select v-model="selectedSpawnerId" :class="fieldClass">
      <option value="">
        {{ selectedProjectId ? 'Project default' : 'Claude default' }}
      </option>
      <option v-for="s in spawners" :key="s.id" :value="s.id">
        {{ s.name }}{{ s.builtIn ? ' (built-in)' : '' }}
      </option>
    </select>

    <PermissionTemplatePicker v-model="selectedTemplate" />

    <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400">
      {{ errorMsg }}
    </p>

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
