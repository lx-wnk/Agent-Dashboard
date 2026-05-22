<script setup lang="ts">
import type { PipelineTask } from '../types'
import { ref } from 'vue'
import { useProjects } from '../composables/useProjects'
import ProjectStep from './backlog/ProjectStep.vue'
import DetailsStep from './backlog/DetailsStep.vue'

const emit = defineEmits<{ created: [task: PipelineTask] }>()

const { projects } = useProjects()
const step = ref<1 | 2>(1)
const selectedProjectId = ref<string>('')
const skipped = ref(false)

function onNext(): void {
  step.value = 2
}

function onBack(): void {
  step.value = 1
}

function onCreated(task: PipelineTask): void {
  emit('created', task)
  step.value = 1
  selectedProjectId.value = ''
  skipped.value = false
}
</script>

<template>
  <div>
    <ProjectStep
      v-if="step === 1"
      :projects="projects"
      :selected-project-id="selectedProjectId"
      :skipped="skipped"
      @update:selected-project-id="selectedProjectId = $event"
      @update:skipped="skipped = $event"
      @next="onNext"
    />
    <DetailsStep
      v-else
      :selected-project-id="selectedProjectId"
      :skipped="skipped"
      @back="onBack"
      @created="onCreated"
    />
  </div>
</template>
