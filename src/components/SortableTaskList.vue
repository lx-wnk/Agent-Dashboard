<script setup lang="ts">
import type { PipelineTask, Project } from '../types'
import { useSortable } from '@vueuse/integrations/useSortable'
import { ref, watch } from 'vue'
import { reorderTask } from '../composables/useTasks'
import TaskCard from './TaskCard.vue'

const props = defineProps<{
  tasks: PipelineTask[]
  projectById: Map<string, Project>
}>()

const emit = defineEmits<{ select: [task: PipelineTask], openChat: [task: PipelineTask] }>()

const listEl = ref<HTMLElement | null>(null)
// Local mirror that Sortable mutates on drop. Kept in sync with the parent's
// rank-sorted list; the optimistic rank update produces the same order, so the
// list resets cleanly when props flow back.
const list = ref<PipelineTask[]>([...props.tasks])
watch(() => props.tasks, v => (list.value = [...v]))

function projectFor(task: PipelineTask): Project | null {
  return task.projectId ? props.projectById.get(task.projectId) ?? null : null
}

useSortable(listEl, list, {
  handle: '.task-drag-handle',
  animation: 150,
  onEnd(evt: { oldIndex?: number, newIndex?: number }) {
    const { oldIndex, newIndex } = evt
    if (oldIndex == null || newIndex == null || oldIndex === newIndex)
      return
    // useSortable's internal onUpdate has already reordered `list` by now.
    const moved = list.value[newIndex]
    if (!moved)
      return
    const beforeId = newIndex > 0 ? list.value[newIndex - 1].id : null
    const afterId = newIndex < list.value.length - 1 ? list.value[newIndex + 1].id : null
    void reorderTask(moved.id, beforeId, afterId)
  },
})
</script>

<template>
  <div ref="listEl" class="flex flex-col gap-2">
    <TaskCard
      v-for="task in list"
      :key="task.id"
      :task="task"
      :project="projectFor(task)"
      @select="(t) => emit('select', t)"
      @open-chat="(t) => emit('openChat', t)"
    />
  </div>
</template>
