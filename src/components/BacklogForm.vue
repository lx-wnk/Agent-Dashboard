<script setup lang="ts">
import type { PipelineTask, ProjectFolder } from '../types'
import { ref, watch } from 'vue'
import { suggestFolders } from '../composables/useProjectFolders'
import { createTask } from '../composables/useTasks'
import { useProjects } from '../composables/useProjects'
import { useSpawners } from '../composables/useSpawners'
import { slugify } from '../utils/validation'
import PermissionTemplatePicker from './PermissionTemplatePicker.vue'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'

const emit = defineEmits<{ created: [task: PipelineTask] }>()

const title = ref('')
const slug = ref('')
const description = ref('')
const cwd = ref('')
const priority = ref<'high' | 'medium' | 'low'>('medium')
type PermissionTemplateId = 'research_only' | 'test_only' | 'review_only' | 'feature_implementation'
const selectedTemplate = ref<PermissionTemplateId | null>('feature_implementation')

// Project / folder / spawner — auto-start so dropdowns populate when the form opens
const { projects } = useProjects()
const { spawners } = useSpawners()
const selectedProjectId = ref<string>('')
const selectedSpawnerId = ref<string>('')
const folderSuggestions = ref<ProjectFolder[]>([])
const cwdDatalistId = 'backlog-cwd-list'

const isSubmitting = ref(false)
const errorMsg = ref('')

function onTitleInput(value: string) {
  title.value = value
  if (!slug.value || slug.value === slugify(title.value.slice(0, -1))) {
    slug.value = slugify(value)
  }
}

// When project changes, load folder suggestions and pre-fill cwd with default
watch(selectedProjectId, async (pid) => {
  folderSuggestions.value = []
  if (!pid)
    return
  try {
    const suggestions = await suggestFolders(pid)
    folderSuggestions.value = suggestions
    // Pre-fill cwd with the default folder if cwd is empty
    const defaultFolder = suggestions.find(f => f.isDefault) ?? suggestions[0]
    if (defaultFolder && !cwd.value.trim()) {
      cwd.value = defaultFolder.path
    }
  }
  catch {
    // ignore — free-text fallback still available
  }
})

async function handleSubmit() {
  if (isSubmitting.value)
    return

  const trimmedTitle = title.value.trim()
  const trimmedSlug = slug.value.trim()
  const trimmedCwd = cwd.value.trim()

  if (!trimmedTitle || !trimmedSlug || !trimmedCwd) {
    errorMsg.value = 'Title, slug, and working directory are required.'
    return
  }

  isSubmitting.value = true
  errorMsg.value = ''

  try {
    const task = await createTask({
      slug: trimmedSlug,
      title: trimmedTitle,
      description: description.value.trim() || undefined,
      cwd: trimmedCwd,
      priority: priority.value,
      template: selectedTemplate.value ?? undefined,
      projectId: selectedProjectId.value || undefined,
      spawnerId: selectedSpawnerId.value || undefined,
    })

    // Reset form
    title.value = ''
    slug.value = ''
    description.value = ''
    cwd.value = ''
    priority.value = 'medium'
    selectedTemplate.value = 'feature_implementation'
    selectedProjectId.value = ''
    selectedSpawnerId.value = ''
    folderSuggestions.value = []

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
  <form class="space-y-4" @submit.prevent="handleSubmit">
    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="backlog-title">
        Title
      </label>
      <AppInput
        id="backlog-title"
        :model-value="title"
        required
        placeholder="What should the agent do?"
        @update:model-value="onTitleInput"
      />
    </div>

    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="backlog-slug">
        Slug
      </label>
      <AppInput
        id="backlog-slug"
        v-model="slug"
        required
        placeholder="task-slug"
      />
    </div>

    <!-- Project dropdown -->
    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="backlog-project">
        Project (optional)
      </label>
      <select
        id="backlog-project"
        v-model="selectedProjectId"
        class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-blue-500"
      >
        <option value="">
          None
        </option>
        <option v-for="p in projects" :key="p.id" :value="p.id">
          {{ p.name }}
        </option>
      </select>
    </div>

    <!-- Working directory — combobox when project is selected, plain input otherwise -->
    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="backlog-cwd">
        Working Directory
      </label>
      <input
        id="backlog-cwd"
        v-model="cwd"
        required
        list="backlog-cwd-list"
        placeholder="/path/to/project"
        class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-blue-500"
      >
      <datalist :id="cwdDatalistId">
        <option v-for="folder in folderSuggestions" :key="folder.id" :value="folder.path">
          {{ folder.label || folder.path }}
        </option>
      </datalist>
    </div>

    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="backlog-description">
        Description
      </label>
      <AppInput
        id="backlog-description"
        v-model="description"
        type="textarea"
        :rows="3"
        placeholder="Additional context (optional)"
      />
    </div>

    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="backlog-priority">
        Priority
      </label>
      <select
        id="backlog-priority"
        v-model="priority"
        class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-blue-500"
      >
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

    <!-- Spawner override dropdown -->
    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="backlog-spawner">
        Spawner Override (optional)
      </label>
      <select
        id="backlog-spawner"
        v-model="selectedSpawnerId"
        class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-blue-500"
      >
        <option value="">
          Project default
        </option>
        <option v-for="s in spawners" :key="s.id" :value="s.id">
          {{ s.name }}{{ s.builtIn ? ' (built-in)' : '' }}
        </option>
      </select>
    </div>

    <PermissionTemplatePicker v-model="selectedTemplate" />

    <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400 leading-snug">
      {{ errorMsg }}
    </p>

    <div class="flex justify-end">
      <AppButton
        variant="primary"
        :disabled="isSubmitting || !title.trim() || !slug.trim() || !cwd.trim()"
        @click="handleSubmit"
      >
        {{ isSubmitting ? 'Creating…' : 'Create Task' }}
      </AppButton>
    </div>
  </form>
</template>
