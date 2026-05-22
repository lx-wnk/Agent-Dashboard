<script setup lang="ts">
import type { PipelineTask } from '../../types'

defineProps<{
  selectedProjectId: string
  skipped: boolean
}>()

defineEmits<{
  back: []
  created: [task: PipelineTask]
}>()
</script>

<template>
  <div data-testid="details-step">
    <p class="text-fg-mute">Details placeholder</p>
  </div>
</template>
