<script setup lang="ts">
import type { Project, Spawner } from '../types'
import { computed, ref, watch } from 'vue'
import { createFolder } from '../composables/useProjectFolders'
import { createProject, deleteProject } from '../composables/useProjects'
import { toast } from '../composables/useToast'
import { errorMessage } from '../utils/errorMessage'
import { slugify } from '../utils/validation'
import AppButton from './ui/AppButton.vue'
import AppSelect from './ui/AppSelect.vue'

const props = defineProps<{ spawners: Spawner[] }>()
const emit = defineEmits<{ created: [project: Project], cancel: [] }>()

const name = ref('')
const path = ref('')
const slug = ref('')
const slugDirty = ref(false)
const description = ref('')
const color = ref('')
const defaultSpawnerId = ref<string>('')
const isSubmitting = ref(false)

const defaultSpawnerSlug = 'claude-default'
const defaultClaudeSpawner = computed(() =>
  props.spawners.find(s => s.slug === defaultSpawnerSlug),
)

watch(name, (v) => {
  if (!slugDirty.value)
    slug.value = slugify(v)
})
watch(defaultClaudeSpawner, (v) => {
  if (v && !defaultSpawnerId.value)
    defaultSpawnerId.value = v.id
}, { immediate: true })

function onSlugInput(e: Event): void {
  const v = (e.target as HTMLInputElement).value
  slug.value = v
  slugDirty.value = v.length > 0
}

const inputClass = 'w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent'

const spawnerOptions = computed(() => [
  { value: '', label: '(none)' },
  ...props.spawners.map(s => ({ value: s.id, label: s.name })),
])

async function submit(): Promise<void> {
  if (isSubmitting.value)
    return
  if (!name.value.trim() || !path.value.trim()) {
    toast.error('Name and Path are required.')
    return
  }
  isSubmitting.value = true

  const projectInput = {
    name: name.value.trim(),
    slug: slug.value.trim() || slugify(name.value),
    ...(description.value.trim() ? { description: description.value.trim() } : {}),
    ...(color.value.trim() ? { color: color.value.trim() } : {}),
    ...(defaultSpawnerId.value ? { defaultSpawnerId: defaultSpawnerId.value } : {}),
  }

  let project: Project
  try {
    project = await createProject(projectInput)
  }
  catch (e) {
    toast.error(errorMessage(e))
    isSubmitting.value = false
    return
  }

  try {
    const folder = await createFolder(project.id, { path: path.value.trim(), isDefault: true })
    emit('created', { ...project, folders: [folder] })
  }
  catch (e) {
    toast.error(errorMessage(e))
    await deleteProject(project.id).catch(() => {})
  }
  finally {
    isSubmitting.value = false
  }
}
</script>

<template>
  <div class="bg-app/60 border border-line rounded p-3 mb-4">
    <h3 class="text-xs font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Create new project
    </h3>
    <form @submit.prevent="submit">
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-name">Name *</label>
        <input
          id="qcp-name"
          v-model="name"
          name="name"
          required
          placeholder="My Project"
          :class="inputClass"
        >
      </div>
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-path">Path *</label>
        <input
          id="qcp-path"
          v-model="path"
          name="path"
          required
          placeholder="/home/me/projects/my-project"
          :class="inputClass"
        >
      </div>
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-slug">Slug</label>
        <input
          id="qcp-slug"
          :value="slug"
          name="slug"
          :class="inputClass"
          placeholder="auto from name"
          @input="onSlugInput"
        >
      </div>
      <div class="mb-2">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-spawner">Default Spawner</label>
        <AppSelect
          id="qcp-spawner"
          v-model="defaultSpawnerId"
          :options="spawnerOptions"
          class="w-full"
        />
      </div>
      <div class="mb-2 flex gap-2">
        <div class="flex-1">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-color">Color</label>
          <input
            id="qcp-color"
            v-model="color"
            name="color"
            type="color"
            class="w-full h-9 bg-app border border-line rounded"
          >
        </div>
        <div class="flex-[3]">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="qcp-desc">Description</label>
          <input
            id="qcp-desc"
            v-model="description"
            name="description"
            placeholder="(optional)"
            :class="inputClass"
          >
        </div>
      </div>
      <div class="flex justify-end gap-2">
        <AppButton type="button" variant="secondary" @click="emit('cancel')">
          Cancel
        </AppButton>
        <AppButton type="submit" variant="primary" :disabled="isSubmitting || !name.trim() || !path.trim()">
          {{ isSubmitting ? 'Creating…' : 'Create' }}
        </AppButton>
      </div>
    </form>
  </div>
</template>
