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
