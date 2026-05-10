<script setup lang="ts">
import type { PipelineTask } from '../types'
import { ref } from 'vue'
import { createTask } from '../composables/useTasks'
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

const isSubmitting = ref(false)
const errorMsg = ref('')

function onTitleInput(value: string) {
  title.value = value
  if (!slug.value || slug.value === slugify(title.value.slice(0, -1))) {
    slug.value = slugify(value)
  }
}

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
    })

    // Reset form
    title.value = ''
    slug.value = ''
    description.value = ''
    cwd.value = ''
    priority.value = 'medium'
    selectedTemplate.value = 'feature_implementation'

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
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="backlog-title">
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
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="backlog-slug">
        Slug
      </label>
      <AppInput
        id="backlog-slug"
        v-model="slug"
        required
        placeholder="task-slug"
      />
    </div>

    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="backlog-cwd">
        Working Directory
      </label>
      <AppInput
        id="backlog-cwd"
        v-model="cwd"
        required
        placeholder="/path/to/project"
      />
    </div>

    <div>
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="backlog-description">
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
      <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="backlog-priority">
        Priority
      </label>
      <select
        id="backlog-priority"
        v-model="priority"
        class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-blue-500"
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
