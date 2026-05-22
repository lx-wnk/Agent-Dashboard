<script setup lang="ts">
import type { Project } from '../../types'
import { computed, ref } from 'vue'
import { useSpawners } from '../../composables/useSpawners'
import QuickCreateProjectPanel from '../QuickCreateProjectPanel.vue'
import AppButton from '../ui/AppButton.vue'

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
        <div class="text-fg font-medium">
          {{ p.name }}
        </div>
        <div class="text-xs text-fg-mute">
          {{ p.folders?.[0]?.path ?? '—' }}
        </div>
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
